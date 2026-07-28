package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/dalemusser/mhsaudiotools/engine/job"
	"github.com/dalemusser/mhsaudiotools/engine/output"
	"github.com/dalemusser/mhsaudiotools/engine/synth"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

func runGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	var (
		in          = fs.String("in", "", "dialog source file (required)")
		adapter     = fs.String("source", "auto", "source format: auto, dbexport, simplescript")
		voicesPath  = fs.String("voices", "", "voices.json config, or a VoiceAssignments.csv (required)")
		outDir      = fs.String("out", "", "output folder (required)")
		layoutName  = fs.String("layout", "dialog-system", "output layout: dialog-system, babylon-manifest")
		format      = fs.String("format", string(synth.MP3_44100_128), "ElevenLabs output_format")
		timestamps  = fs.Bool("timestamps", false, "also produce word timings (<id>.words.json)")
		concurrency = fs.Int("concurrency", 0, "simultaneous requests (0 = auto-detect the account's max)")
		force       = fs.Bool("force", false, "regenerate everything, ignoring existing audio")
		dryRun      = fs.Bool("dry-run", false, "show what would be generated; makes no API calls")
		profilePath = fs.String("profile", "", "cleanup profile JSON (default: built-in mhs-dialogue)")
		noCleanup   = fs.Bool("no-cleanup", false, "disable text cleanup entirely")
		model       = fs.String("model", "", "model for voices that don't set one in the voices file: v2 (default), v3, or a full ElevenLabs model ID")
		emotionOn   = fs.Bool("emotion", false, "turn the writers' (sighs)/(angry) directions into v3 audio tags; on v2 files the directions are stripped from the spoken text")
		emotionPath = fs.String("emotion-map", "", "emotion map JSON (default: built-in mhs-emotion; implies -emotion)")
		defSpeaker  = fs.String("default-speaker", "", "character whose voice speaks lines that have no speaker (e.g. Toppo)")
		defVoice    = fs.String("default-voice", "", "voice ID for lines that have no speaker")
		keyFile     = fs.String("key-file", "", "file holding the ElevenLabs API key")
		verbose     = fs.Bool("v", false, "list every file in the dry run")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Generate audio for a dialog source.\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n"+
			"  mhsaudio generate -in export.csv -voices voices.json -out ./audio -dry-run\n"+
			"  mhsaudio generate -in export.csv -voices voices.json -out ./audio\n"+
			"  mhsaudio generate -in lessons.txt -voices voices.csv -out ./lessons -default-speaker Toppo\n"+
			"  mhsaudio generate -in export.csv -voices voices.json -out ./audio -emotion -dry-run\n"+
			"  mhsaudio generate -in export.csv -voices voices.json -out ./audio -model v3 -emotion\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *voicesPath == "" || *outDir == "" {
		fs.Usage()
		return fmt.Errorf("-in, -voices and -out are required")
	}

	lines, srcName, err := loadLines(*in, *adapter)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("no dialog lines found in %s", *in)
	}

	cfg, err := loadVoiceConfig(*voicesPath)
	if err != nil {
		return err
	}
	if problems := cfg.Validate(); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "warning: voices: %s\n", p)
		}
	}
	if err := applyDefaultVoice(cfg, *defSpeaker, *defVoice); err != nil {
		return err
	}

	profile, err := loadProfile(*profilePath, *noCleanup)
	if err != nil {
		return err
	}
	emo, err := loadEmotionMap(*emotionPath, *emotionOn)
	if err != nil {
		return err
	}
	layout, err := pickLayout(*layoutName)
	if err != nil {
		return err
	}
	defaultModel := resolveModel(*model)

	runner := &job.Runner{
		Cleanup: profile,
		Voices:  cfg,
		Layout:  layout,
		Emotion: emo,
		Options: job.Options{
			OutputDir:      *outDir,
			Format:         synth.AudioFormat(*format),
			WithTimestamps: *timestamps,
			Concurrency:    *concurrency,
			Force:          *force,
			DefaultModel:   defaultModel,
		},
	}

	// Plan once, up front: it needs no API key, surfaces a corrupt manifest
	// before anything else, and the header's model/emotion facts come from the
	// files actually planned — not merely what the voices file configures.
	plan, err := runner.Plan(lines)
	if err != nil {
		return err
	}
	v3Files := 0
	for _, it := range plan.Items {
		if it.Model == synth.ModelV3 {
			v3Files++
		}
	}

	// Header (shared by dry runs and real runs).
	fmt.Printf("Source:   %s (%s) — %s lines\n", *in, srcName, commas(len(lines)))
	fmt.Printf("Voices:   %d characters, %d player slots\n", len(cfg.Assignments), len(cfg.PlayerSlots))
	if profile != nil {
		fmt.Printf("Cleanup:  %s (%d rules)\n", profile.Name, len(profile.Rules))
	} else {
		fmt.Printf("Cleanup:  disabled\n")
	}
	fmt.Printf("Models:   %d of %d files render v3 (default %s)\n",
		v3Files, len(plan.Items), modelShort(firstNonEmpty(defaultModel, synth.ModelV2)))
	if emo != nil {
		fmt.Printf("Emotion:  %s (%d tags — tags apply to v3 files; directions are stripped everywhere)\n",
			emo.Name, len(emo.Tags))
		if v3Files == 0 {
			fmt.Fprintf(os.Stderr, "note: no file renders on v3, so no audio tags will be applied — "+
				"the writers' (sighs)/(angry) directions are still removed from the spoken text, "+
				"which changes it and regenerates affected lines\n")
		}
	}
	fmt.Printf("Output:   %s  [layout %s, format %s, timestamps %v]\n\n",
		*outDir, layout.Name(), *format, *timestamps)

	if *dryRun {
		return dryRunPlan(plan, *verbose)
	}

	key, err := resolveAPIKey(*keyFile)
	if err != nil {
		return err
	}
	runner.Client = synth.NewElevenLabs(key)

	// Ctrl-C cancels cleanly: in-flight work stops and the manifest is saved, so
	// a re-run resumes rather than starting over. Once the first signal has
	// canceled the context, release the handler so a second Ctrl-C force-quits
	// instead of being swallowed if teardown ever stalls.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); stop() }()

	runner.Options.Concurrency = resolveConcurrency(ctx, runner, *concurrency)

	pr := newProgressPrinter()
	runner.OnProgress = pr.update

	start := time.Now()
	res, runErr := runner.Run(ctx, lines)
	pr.done()

	if res != nil {
		printResult(res, time.Since(start))
	}
	if runErr != nil {
		// The run's own verdict, not the ctx state at this instant — a Ctrl-C
		// racing a genuine failure must not relabel it and swallow the error.
		if errors.Is(runErr, context.Canceled) {
			return fmt.Errorf("canceled — re-run to resume where it stopped")
		}
		return runErr
	}
	if res != nil && res.Failed > 0 {
		// Scripts chain on the exit code; a run with failures must not exit 0.
		return fmt.Errorf("%s of %s files failed — re-run to retry just those", commas(res.Failed), commas(res.Targets))
	}
	return nil
}

// dryRunPlan previews the run without touching the API — the cheap sanity check
// before committing to thousands of paid requests.
func dryRunPlan(plan *job.PlanResult, verbose bool) error {
	fmt.Printf("DRY RUN — no API calls, nothing written\n\n")
	fmt.Printf("  lines:          %s parsed, %s cleaned to nothing\n",
		commas(plan.Lines), commas(plan.SkippedLines))
	fmt.Printf("  audio files:    %s total\n", commas(plan.Targets))
	fmt.Printf("    to generate:  %s\n", commas(plan.ToGenerate))
	fmt.Printf("    up to date:   %s\n", commas(plan.UpToDate))
	fmt.Printf("  characters:     %s to synthesize\n", commas(plan.Characters))
	if plan.Failed > 0 {
		fmt.Printf("  problems:       %s\n", commas(plan.Failed))
	}

	// Per-voice breakdown: the quickest way to spot a miscast character — and,
	// with the model column, which voices render v3 (and so get emotion tags).
	// Two characters can share one ElevenLabs voice name on different models;
	// that shows as "mixed" rather than whichever target happened to come last.
	byVoice := map[string]int{}
	voiceModel := map[string]string{}
	for _, it := range plan.Items {
		if it.UpToDate {
			continue
		}
		name := it.VoiceName
		if name == "" {
			name = it.VoiceID
		}
		byVoice[name]++
		m := modelShort(it.Model)
		if prev, ok := voiceModel[name]; ok && prev != m {
			m = "mixed"
		}
		voiceModel[name] = m
	}
	if len(byVoice) > 0 {
		w := 4
		for _, m := range voiceModel {
			if len(m) > w {
				w = len(m)
			}
		}
		fmt.Printf("\n  files per voice:\n")
		names := make([]string, 0, len(byVoice))
		for n := range byVoice {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("    %-22s %-*s %s\n", n, w, voiceModel[n], commas(byVoice[n]))
		}
	}

	if len(plan.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\n  problems:\n")
		for i, e := range plan.Errors {
			if i == 20 {
				fmt.Fprintf(os.Stderr, "    … and %d more\n", len(plan.Errors)-20)
				break
			}
			fmt.Fprintf(os.Stderr, "    %s\n", e.Error())
		}
	}

	if verbose {
		fmt.Printf("\n  files:\n")
		for _, it := range plan.Items {
			status := "generate"
			if it.UpToDate {
				status = "skip    "
			}
			fmt.Printf("    %s  %-34s %-4s [%s]\n", status, it.RelPath, modelShort(it.Model), it.VoiceName)
		}
	} else if n := len(plan.Items); n > 0 {
		fmt.Printf("\n  sample (-v for all %s):\n", commas(n))
		for i, it := range plan.Items {
			if i == 5 {
				break
			}
			fmt.Printf("    %-34s %-4s [%s]  %.44q\n", it.RelPath, modelShort(it.Model), it.VoiceName, it.Text)
		}
	}
	if plan.Failed > 0 {
		// Preflight checks are scriptable; problems must not exit 0.
		return fmt.Errorf("dry run found %d problem(s)", plan.Failed)
	}
	return nil
}

func pickLayout(name string) (output.Layout, error) {
	switch name {
	case "dialog-system":
		return output.DialogSystem{}, nil
	case "babylon-manifest":
		return output.BabylonManifest{}, nil
	default:
		return nil, fmt.Errorf("unknown layout %q (have: dialog-system, babylon-manifest)", name)
	}
}

// applyDefaultVoice sets the fallback voice for lines with no speaker, either by
// naming a character in the config or by giving a raw voice ID.
func applyDefaultVoice(cfg *voice.Config, speaker, voiceID string) error {
	switch {
	case speaker != "" && voiceID != "":
		return fmt.Errorf("-default-speaker and -default-voice are mutually exclusive; pass one")
	case speaker != "":
		a, ok := cfg.VoiceFor(speaker)
		if !ok {
			return fmt.Errorf("-default-speaker %q has no voice assignment", speaker)
		}
		cfg.Default = &a
	case voiceID != "":
		cfg.Default = &voice.Assignment{VoiceID: voiceID, VoiceName: voiceID}
	}
	return nil
}

// resolveConcurrency picks the worker-pool size once, prints it, and returns it
// for Options.Concurrency — so the number reported and the number used cannot
// diverge (the runner would otherwise re-resolve it with a second API call that
// could fail differently).
func resolveConcurrency(ctx context.Context, runner *job.Runner, requested int) int {
	if requested > 0 {
		fmt.Printf("Parallel: %d (set explicitly)\n", requested)
		return requested
	}
	sub, err := runner.Client.Subscription(ctx)
	if err != nil {
		fmt.Printf("Parallel: %d (couldn't read the account; using a safe default)\n", synth.ConservativeConcurrency)
		return synth.ConservativeConcurrency
	}
	n, known := synth.MaxConcurrency(sub.Tier)
	if !known {
		fmt.Printf("Parallel: %d (tier %q not recognized; using a safe default)\n", synth.ConservativeConcurrency, sub.Tier)
		return synth.ConservativeConcurrency
	}
	fmt.Printf("Parallel: %d (max for tier %q)\n", n, sub.Tier)
	return n
}

func printResult(res *job.Result, elapsed time.Duration) {
	fmt.Printf("\nDone in %s\n", elapsed.Round(time.Second))
	fmt.Printf("  files written:  %s\n", commas(res.Written))
	fmt.Printf("  up to date:     %s\n", commas(res.SkippedFiles))
	fmt.Printf("  lines skipped:  %s (nothing to say after cleanup)\n", commas(res.SkippedLines))
	fmt.Printf("  failed:         %s\n", commas(res.Failed))
	if len(res.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\nFailures:\n")
		for i, e := range res.Errors {
			if i == 20 {
				fmt.Fprintf(os.Stderr, "  … and %d more\n", len(res.Errors)-20)
				break
			}
			fmt.Fprintf(os.Stderr, "  %s\n", e.Error())
		}
	}
	fmt.Printf("\nManifest: %s\n", res.ManifestPath)
}

// progressPrinter renders a single rewriting status line. OnProgress is called
// from every worker, so updates are serialized and throttled.
type progressPrinter struct {
	mu   sync.Mutex
	last time.Time
	seen bool
}

func newProgressPrinter() *progressPrinter { return &progressPrinter{} }

func (p *progressPrinter) update(pr job.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pr.Total == 0 {
		return
	}
	// Throttle to keep the terminal readable during thousands of files.
	if pr.Done < pr.Total && time.Since(p.last) < 100*time.Millisecond {
		return
	}
	p.last = time.Now()
	p.seen = true
	pct := pr.Done * 100 / pr.Total
	fmt.Printf("\r  %s/%s files (%d%%)  failed %d   ",
		commas(pr.Done), commas(pr.Total), pct, pr.Failed)
}

func (p *progressPrinter) done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen {
		fmt.Println()
	}
}
