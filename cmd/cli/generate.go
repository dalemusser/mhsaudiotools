package main

import (
	"context"
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
	"github.com/dalemusser/mhsaudiotools/engine/source"
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
		defSpeaker  = fs.String("default-speaker", "", "character whose voice speaks lines that have no speaker (e.g. Toppo)")
		defVoice    = fs.String("default-voice", "", "voice ID for lines that have no speaker")
		keyFile     = fs.String("key-file", "", "file holding the ElevenLabs API key")
		verbose     = fs.Bool("v", false, "list every file in the dry run")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Generate audio for a dialog source.\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n"+
			"  aigenaudio generate -in export.csv -voices voices.json -out ./audio -dry-run\n"+
			"  aigenaudio generate -in export.csv -voices voices.json -out ./audio\n"+
			"  aigenaudio generate -in lessons.txt -voices voices.csv -out ./lessons -default-speaker Toppo\n")
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
	layout, err := pickLayout(*layoutName)
	if err != nil {
		return err
	}

	runner := &job.Runner{
		Cleanup: profile,
		Voices:  cfg,
		Layout:  layout,
		Options: job.Options{
			OutputDir:      *outDir,
			Format:         synth.AudioFormat(*format),
			WithTimestamps: *timestamps,
			Concurrency:    *concurrency,
			Force:          *force,
		},
	}

	// Header (shared by dry runs and real runs).
	fmt.Printf("Source:   %s (%s) — %s lines\n", *in, srcName, commas(len(lines)))
	fmt.Printf("Voices:   %d characters, %d player slots\n", len(cfg.Assignments), len(cfg.PlayerSlots))
	if profile != nil {
		fmt.Printf("Cleanup:  %s (%d rules)\n", profile.Name, len(profile.Rules))
	} else {
		fmt.Printf("Cleanup:  disabled\n")
	}
	fmt.Printf("Output:   %s  [layout %s, format %s, timestamps %v]\n\n",
		*outDir, layout.Name(), *format, *timestamps)

	if *dryRun {
		return dryRunPlan(runner, lines, *verbose)
	}

	key, err := resolveAPIKey(*keyFile)
	if err != nil {
		return err
	}
	runner.Client = synth.NewElevenLabs(key)

	// Ctrl-C cancels cleanly: in-flight work stops and the manifest is saved, so
	// a re-run resumes rather than starting over.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reportConcurrency(ctx, runner, *concurrency)

	pr := newProgressPrinter()
	runner.OnProgress = pr.update

	start := time.Now()
	res, runErr := runner.Run(ctx, lines)
	pr.done()

	if res != nil {
		printResult(res, time.Since(start))
	}
	if runErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("canceled — re-run to resume where it stopped")
		}
		return runErr
	}
	return nil
}

// dryRunPlan previews the run without touching the API — the cheap sanity check
// before committing to thousands of paid requests.
func dryRunPlan(runner *job.Runner, lines []source.LineItem, verbose bool) error {
	plan, err := runner.Plan(lines)
	if err != nil {
		return err
	}

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

	// Per-voice breakdown: the quickest way to spot a miscast character.
	byVoice := map[string]int{}
	for _, it := range plan.Items {
		if it.UpToDate {
			continue
		}
		name := it.VoiceName
		if name == "" {
			name = it.VoiceID
		}
		byVoice[name]++
	}
	if len(byVoice) > 0 {
		fmt.Printf("\n  files per voice:\n")
		names := make([]string, 0, len(byVoice))
		for n := range byVoice {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("    %-22s %s\n", n, commas(byVoice[n]))
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
			fmt.Printf("    %s  %-34s [%s]\n", status, it.RelPath, it.VoiceName)
		}
	} else if n := len(plan.Items); n > 0 {
		fmt.Printf("\n  sample (-v for all %s):\n", commas(n))
		for i, it := range plan.Items {
			if i == 5 {
				break
			}
			fmt.Printf("    %-34s [%s]  %.44q\n", it.RelPath, it.VoiceName, it.Text)
		}
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

// reportConcurrency tells the user how many requests will run in parallel and why.
func reportConcurrency(ctx context.Context, runner *job.Runner, requested int) {
	if requested > 0 {
		fmt.Printf("Parallel: %d (set explicitly)\n", requested)
		return
	}
	sub, err := runner.Client.Subscription(ctx)
	if err != nil {
		fmt.Printf("Parallel: %d (couldn't read the account; using a safe default)\n", synth.ConservativeConcurrency)
		return
	}
	n, known := synth.MaxConcurrency(sub.Tier)
	if !known {
		fmt.Printf("Parallel: %d (tier %q not recognized; using a safe default)\n", synth.ConservativeConcurrency, sub.Tier)
		return
	}
	fmt.Printf("Parallel: %d (max for tier %q)\n", n, sub.Tier)
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
