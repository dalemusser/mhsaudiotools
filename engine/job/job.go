// Package job runs a generation batch: it plans the work (clean text, resolve
// voices, expand player fan-out, drop what's already up to date), then executes
// it through a bounded worker pool with progress reporting and cancellation.
// record.go persists per-run records (shared between the CLI and the app) so
// runs survive a restart and can be listed, resumed, and expired.
package job

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dalemusser/mhsaudiotools/engine/emotion"
	"github.com/dalemusser/mhsaudiotools/engine/output"
	"github.com/dalemusser/mhsaudiotools/engine/pron"
	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/synth"
	"github.com/dalemusser/mhsaudiotools/engine/text"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// manifestName records what each written file was generated from, enabling
// idempotent re-runs (regenerate only what changed).
const manifestName = ".mhsaudio-manifest.json"

// manifestFlushEvery is how many written files trigger an incremental manifest
// save during a run, bounding what a crash can lose to a handful of files.
const manifestFlushEvery = 25

// Options configures a generation run.
type Options struct {
	OutputDir      string
	Format         synth.AudioFormat
	WithTimestamps bool

	// DefaultModel is the ElevenLabs model for voices that don't specify one
	// (empty falls back to synth.ModelV2). A voice's own Model overrides this.
	DefaultModel string

	// Concurrency is the number of simultaneous API requests. Leave it 0 (or
	// negative) to auto-detect the account's tier and use its full documented
	// cap — the right default, since a full run is thousands of files and the
	// cap is the only thing limiting throughput. Set it explicitly to dial back
	// (e.g. when teammates share the key and run at the same time).
	Concurrency int

	Force bool // regenerate everything, ignoring existing audio/hashes
}

// Progress is a snapshot of a run, reported as units complete.
type Progress struct {
	Total  int // audio files to synthesize (after skips)
	Done   int
	Failed int
}

// LineError records a failure for one line or one of its output files.
type LineError struct {
	LineID  string
	RelPath string
	Err     error
}

func (e LineError) Error() string {
	if e.RelPath != "" {
		return fmt.Sprintf("%s (%s): %v", e.LineID, e.RelPath, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.LineID, e.Err)
}

// Result summarizes a finished run. Failed counts both planning failures
// (unknown speaker, bad cleanup rule) and synthesis/write failures.
type Result struct {
	Lines        int // input lines
	Targets      int // audio files implied (player lines fan out across slots)
	Written      int
	SkippedLines int // lines with no text left after cleanup
	SkippedFiles int // files already up to date
	Failed       int
	Errors       []LineError
	ManifestPath string

	// LayoutManifest is the consumer-facing manifest path, set when the layout
	// emits one (Babylon's ceremony_audio.json).
	LayoutManifest string

	// WrittenFiles are the output-relative paths THIS run wrote: audio, any
	// .words.json sidecars, and the layout manifest when one was emitted —
	// exactly the set to import into a game project after an incremental run.
	WrittenFiles []string
}

// Runner executes a generation run over normalized lines.
type Runner struct {
	Client    synth.Client
	Cleanup   *text.Profile // optional; nil means no text cleanup
	Voices    *voice.Config
	Layout    output.Layout
	Emotion   *emotion.Map    // optional; when set, v3 voices get audio tags from directions
	Overrides voice.Overrides // optional; per-line delivery tweaks keyed by line ID

	// Pronunciations, when active, attaches the published dictionary to every
	// request — the text keeps the writers' spelling and the server fixes the
	// pronunciation, so timings/captions align to the displayed text. The set
	// must be published (shells do that) before Run.
	Pronunciations *pron.Set

	Options Options

	// OnProgress, if set, is called as units complete. It must be safe to call
	// from multiple goroutines and should not block.
	OnProgress func(Progress)
}

// unit is one audio file to synthesize and write. Text and model are per-target:
// a v3 voice gets emotion tags a v2 voice doesn't, so they differ across a player
// line's slots.
type unit struct {
	lineID   string
	relPath  string
	voiceID  string
	model    string
	text     string
	settings *synth.VoiceSettings
	ent      entry
}

// PlanItem is one audio file a run would produce.
type PlanItem struct {
	LineID    string
	Speaker   string
	RelPath   string
	VoiceID   string
	VoiceName string
	Settings  *synth.VoiceSettings // effective knobs (voice + line override); nil = defaults
	Model     string               // the model this file renders with
	Text      string               // the exact text that will be sent (incl. any v3 audio tags)
	UpToDate  bool                 // already generated from this exact text; would be skipped
}

// PlanResult reports what Run would do, without calling the API — the basis for
// a dry run and for a UI preview before committing to a long, paid batch.
type PlanResult struct {
	Lines        int
	SkippedLines int // lines with no text left after cleanup
	Targets      int // audio files implied (player lines fan out across slots)
	ToGenerate   int
	UpToDate     int
	Failed       int
	Errors       []LineError
	Items        []PlanItem
	Characters   int // characters that would be sent to the API

	// Orphans are files the manifest recorded (so this tool created them)
	// whose paths no target of the current plan produces — audio for lines
	// since removed from the source. Computed only when the plan has zero
	// problems: a failed line plans no targets, and its perfectly good files
	// must not be mistaken for orphans.
	Orphans []string
}

// Plan reports what Run would do. It needs no API client and makes no network
// calls, so it is safe to run without a key.
func (r *Runner) Plan(lines []source.LineItem) (*PlanResult, error) {
	if r.Voices == nil {
		return nil, errors.New("job: no voice config configured")
	}
	if r.Layout == nil {
		return nil, errors.New("job: no output layout configured")
	}
	man, err := loadManifest(r.Options.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("job: loading manifest: %w", err)
	}
	return r.planWith(lines, man), nil
}

// Run plans and executes the batch. It returns a Result even when ctx is
// canceled, so callers can report partial progress alongside the error.
func (r *Runner) Run(ctx context.Context, lines []source.LineItem) (*Result, error) {
	if r.Client == nil {
		return nil, errors.New("job: no synth client configured")
	}
	if r.Voices == nil {
		return nil, errors.New("job: no voice config configured")
	}
	if r.Layout == nil {
		return nil, errors.New("job: no output layout configured")
	}
	if r.Options.OutputDir == "" {
		return nil, errors.New("job: no output directory configured")
	}
	if r.Pronunciations.Active() && r.Pronunciations.NeedsPublish() {
		return nil, errors.New("job: pronunciation rules are not published — publish the dictionary before running")
	}

	man, err := loadManifest(r.Options.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("job: loading manifest: %w", err)
	}

	p := r.planWith(lines, man)
	res := &Result{
		Lines:        p.Lines,
		Targets:      p.Targets,
		SkippedLines: p.SkippedLines,
		SkippedFiles: p.UpToDate,
		Failed:       p.Failed,
		Errors:       p.Errors,
	}
	units := make([]unit, 0, p.ToGenerate)
	for _, it := range p.Items {
		if it.UpToDate {
			continue
		}
		units = append(units, unit{
			lineID:   it.LineID,
			relPath:  it.RelPath,
			voiceID:  it.VoiceID,
			model:    it.Model,
			text:     it.Text,
			settings: it.Settings,
			ent:      r.entryFor(it.VoiceID, it.Model, it.Text, it.Settings),
		})
	}

	if err := r.execute(ctx, units, man, res); err != nil {
		// Save what we managed to write before surfacing the error.
		_ = man.save(r.Options.OutputDir)
		res.ManifestPath = filepath.Join(r.Options.OutputDir, manifestName)
		return res, err
	}
	if err := man.save(r.Options.OutputDir); err != nil {
		return res, fmt.Errorf("job: saving manifest: %w", err)
	}
	res.ManifestPath = filepath.Join(r.Options.OutputDir, manifestName)

	// A layout with a consumer-facing manifest (Babylon's ceremony player)
	// gets it after a completed run, covering every non-failed target so an
	// incremental run still describes the whole set.
	if w, ok := r.Layout.(output.RunManifestWriter); ok {
		failedRel := make(map[string]bool, len(res.Errors))
		for _, e := range res.Errors {
			if e.RelPath != "" {
				failedRel[e.RelPath] = true
			}
		}
		files := make([]output.RunManifestFile, 0, len(p.Items))
		for i, it := range p.Items {
			if failedRel[it.RelPath] {
				continue
			}
			files = append(files, output.RunManifestFile{
				Index:    i,
				ID:       it.LineID,
				Speaker:  it.Speaker,
				RelPath:  it.RelPath,
				TextHash: hashText(it.Text),
			})
		}
		mp, err := w.WriteRunManifest(r.Options.OutputDir, files)
		if err != nil {
			return res, fmt.Errorf("job: writing layout manifest: %w", err)
		}
		res.LayoutManifest = mp
		if res.Written > 0 {
			res.WrittenFiles = append(res.WrittenFiles, filepath.Base(mp))
		}
	}
	sort.Strings(res.WrittenFiles)
	return res, nil
}

// PruneOrphans deletes audio this tool created for lines no longer in the
// source: files the manifest recorded that the current plan doesn't produce,
// plus their timing sidecars and newly empty folders. It refuses to run when
// the plan has any problem — a typo'd voices file must never turn half the
// folder "orphaned" — and touches nothing the manifest doesn't know about.
// Needs no API client.
func (r *Runner) PruneOrphans(lines []source.LineItem) ([]string, error) {
	if r.Options.OutputDir == "" {
		return nil, errors.New("job: no output directory configured")
	}
	man, err := loadManifest(r.Options.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("job: loading manifest: %w", err)
	}
	p := r.planWith(lines, man)
	if p.Failed > 0 {
		return nil, fmt.Errorf("job: refusing to prune with %d planning problem(s) — fix them first", p.Failed)
	}
	if len(p.Orphans) == 0 {
		return nil, nil
	}

	base := filepath.Clean(r.Options.OutputDir)
	var pruneErr error
	deleted := make([]string, 0, len(p.Orphans))
	for _, rel := range p.Orphans {
		full := filepath.Join(base, filepath.FromSlash(rel))
		// Same containment guard as writing: a hostile manifest entry must not
		// reach outside the output folder.
		if rp, err := filepath.Rel(base, full); err != nil || rp == ".." || strings.HasPrefix(rp, ".."+string(os.PathSeparator)) {
			continue
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			pruneErr = fmt.Errorf("job: removing %s: %w", rel, err)
			break
		}
		side := full[:len(full)-len(filepath.Ext(full))] + ".words.json"
		if err := os.Remove(side); err != nil && !os.IsNotExist(err) {
			pruneErr = fmt.Errorf("job: removing sidecar for %s: %w", rel, err)
			break
		}
		man.remove(rel)
		deleted = append(deleted, rel)
		removeEmptyDirs(base, filepath.Dir(full))
	}

	// Persist what was actually removed even when a deletion failed midway —
	// the manifest must never claim files that are gone.
	if err := man.save(base); err != nil && pruneErr == nil {
		pruneErr = fmt.Errorf("job: saving manifest after prune: %w", err)
	}
	return deleted, pruneErr
}

// removeEmptyDirs removes dir and its now-empty parents, stopping at base or
// the first non-empty directory (os.Remove fails on those, which is the signal).
func removeEmptyDirs(base, dir string) {
	for dir != base && strings.HasPrefix(dir, base+string(os.PathSeparator)) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// CopyFiles copies relPaths (slash-separated, relative to srcDir) into dstDir,
// preserving the folder layout — the "changed files only" export a game
// project imports after an incremental run, so Perforce/Unity only sees the
// files that actually changed.
func CopyFiles(srcDir, dstDir string, relPaths []string) error {
	if dstDir == "" {
		return errors.New("job: no destination folder")
	}
	for _, rel := range relPaths {
		src := filepath.Join(srcDir, filepath.FromSlash(rel))
		dst := filepath.Join(dstDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("job: reading %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("job: writing %s: %w", rel, err)
		}
	}
	return nil
}

// planWith works out what the run would produce: it cleans each line's text,
// drops lines that clean to nothing, resolves voicing (fanning player lines out
// across slots), and marks targets already up to date.
func (r *Runner) planWith(lines []source.LineItem, man *manifest) *PlanResult {
	ext := extForFormat(r.Options.Format)
	p := &PlanResult{Lines: len(lines)}

	for _, li := range lines {
		// IDs become filenames verbatim; refuse ones that would nest, collide,
		// or escape the output folder.
		if err := checkID(li.ID); err != nil {
			p.Errors = append(p.Errors, LineError{LineID: li.ID, Err: err})
			p.Failed++
			continue
		}

		// Pull the writers' directions out first (so cleanup doesn't eat them),
		// then clean the remaining text. Directions come from inline parentheticals
		// and the source's structured Direction field.
		base := li.Text
		var directions []string
		if r.Emotion != nil {
			base, directions = emotion.Extract(li.Text)
		}
		if li.Direction != "" {
			directions = append(directions, li.Direction)
		}
		if r.Cleanup != nil {
			cleaned, err := r.Cleanup.Apply(base)
			if err != nil {
				p.Errors = append(p.Errors, LineError{LineID: li.ID, Err: err})
				p.Failed++
				continue
			}
			base = cleaned
		}
		base = strings.TrimSpace(base)
		if base == "" {
			p.SkippedLines++ // nothing left to say after cleanup
			continue
		}

		v, err := r.Voices.Resolve(li.Speaker, li.IsPlayer())
		if err != nil {
			p.Errors = append(p.Errors, LineError{LineID: li.ID, Err: err})
			p.Failed++
			continue
		}
		targets, err := r.Layout.Targets(li, v, ext)
		if err != nil {
			p.Errors = append(p.Errors, LineError{LineID: li.ID, Err: err})
			p.Failed++
			continue
		}

		ov, hasOv := r.Overrides[li.ID]
		for _, tg := range targets {
			// Layouts splice in more than the ID (Babylon uses the speaker as a
			// folder), so vet the full path they built, not just the ID.
			if err := checkRelPath(tg.RelPath); err != nil {
				p.Errors = append(p.Errors, LineError{LineID: li.ID, RelPath: tg.RelPath, Err: err})
				p.Failed++
				continue
			}
			p.Targets++
			// Text and model are per-target: only v3 voices get the emotion tags.
			model := r.modelFor(tg.Model)
			text := base
			if r.Emotion != nil && model == synth.ModelV3 {
				text = r.Emotion.Apply(base, directions)
			}
			eff := effectiveSettings(tg.Settings, ov, hasOv)
			item := PlanItem{
				LineID:    li.ID,
				Speaker:   li.Speaker,
				RelPath:   tg.RelPath,
				VoiceID:   tg.VoiceID,
				VoiceName: tg.VoiceName,
				Settings:  eff,
				Model:     model,
				Text:      text,
			}
			if !r.Options.Force && man.upToDate(r.Options.OutputDir, tg.RelPath, r.entryFor(tg.VoiceID, model, text, eff)) {
				item.UpToDate = true
				p.UpToDate++
			} else {
				p.ToGenerate++
				p.Characters += utf8.RuneCountInString(text)
			}
			p.Items = append(p.Items, item)
		}
	}

	if p.Failed == 0 {
		planned := make(map[string]bool, len(p.Items))
		for _, it := range p.Items {
			planned[it.RelPath] = true
		}
		man.mu.Lock()
		for rel := range man.Files {
			if !planned[rel] {
				p.Orphans = append(p.Orphans, rel)
			}
		}
		man.mu.Unlock()
		sort.Strings(p.Orphans)
	}
	return p
}

// checkID rejects line IDs that can't be used as filenames — IDs become
// filenames verbatim, so a bad one nests, collides, escapes the output folder,
// or breaks on Windows.
func checkID(id string) error {
	if err := checkFilename(id); err != nil {
		return fmt.Errorf("line ID %v", err)
	}
	return nil
}

// checkRelPath vets a layout-built output path: every segment must be a plain,
// portable filename.
func checkRelPath(rel string) error {
	for _, seg := range strings.Split(rel, "/") {
		if err := checkFilename(seg); err != nil {
			return err
		}
	}
	return nil
}

// checkFilename rejects names that misbehave on some supported OS: path
// separators (nest or collide silently), dot navigation (escapes the output
// folder), NTFS drive/stream colons, and Windows reserved device names or
// trailing dots/spaces.
func checkFilename(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("%q is not a usable filename", name)
	}
	if strings.ContainsAny(name, `/\:`) {
		return fmt.Errorf("%q contains a path separator or colon", name)
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("%q ends with a dot or space (breaks on Windows)", name)
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return fmt.Errorf("%q is a reserved Windows device name", name)
	}
	return nil
}

// entryFor captures everything that determines a target's rendered content; if
// any of it changes on a later run, the file regenerates.
func (r *Runner) entryFor(voiceID, model, text string, s *synth.VoiceSettings) entry {
	f := string(r.Options.Format)
	if f == "" {
		f = string(synth.MP3_44100_128)
	}
	return entry{
		Text:     hashText(text),
		Voice:    voiceID,
		Model:    model,
		Format:   f,
		Words:    r.Options.WithTimestamps,
		Settings: settingsKey(s),
		// Keyed per line: only lines a rule actually touches regenerate when
		// the pronunciation rules change.
		Dict: r.Pronunciations.AffectedKey(text),
	}
}

// effectiveSettings merges a line's override onto its voice's settings; nil
// means nothing is sent and the voice's ElevenLabs defaults apply.
func effectiveSettings(vs *voice.Settings, ov voice.LineOverride, hasOv bool) *synth.VoiceSettings {
	if vs == nil && !hasOv {
		return nil
	}
	out := &synth.VoiceSettings{}
	if vs != nil {
		out.Stability = vs.Stability
		out.SimilarityBoost = vs.SimilarityBoost
		out.Style = vs.Style
		out.UseSpeakerBoost = vs.SpeakerBoost
		out.Speed = vs.Speed
	}
	if hasOv {
		if ov.Stability != nil {
			out.Stability = ov.Stability
		}
		if ov.Style != nil {
			out.Style = ov.Style
		}
		if ov.Speed != nil {
			out.Speed = ov.Speed
		}
	}
	if out.Stability == nil && out.SimilarityBoost == nil && out.Style == nil &&
		out.UseSpeakerBoost == nil && out.Speed == nil {
		return nil // an empty override object adds nothing
	}
	return out
}

// settingsKey canonicalizes effective settings for the manifest, so changing
// any knob regenerates exactly the affected files. "" means none were sent.
func settingsKey(s *synth.VoiceSettings) string {
	if s == nil {
		return ""
	}
	f := func(p *float64) string {
		if p == nil {
			return "-"
		}
		return strconv.FormatFloat(*p, 'f', -1, 64)
	}
	b := "-"
	if s.UseSpeakerBoost != nil {
		b = strconv.FormatBool(*s.UseSpeakerBoost)
	}
	return fmt.Sprintf("st:%s|si:%s|sy:%s|b:%s|sp:%s",
		f(s.Stability), f(s.SimilarityBoost), f(s.Style), b, f(s.Speed))
}

// modelFor resolves a target's model: the voice's own, else the run default,
// else v2.
func (r *Runner) modelFor(voiceModel string) string {
	if voiceModel != "" {
		return voiceModel
	}
	if r.Options.DefaultModel != "" {
		return r.Options.DefaultModel
	}
	return synth.ModelV2
}

// execute runs the units through a bounded worker pool.
func (r *Runner) execute(ctx context.Context, units []unit, man *manifest, res *Result) error {
	total := len(units)
	r.report(Progress{Total: total})
	if total == 0 {
		return nil
	}

	conc := r.concurrency(ctx)
	if conc > total {
		conc = total
	}

	// One mutex serializes counters, error collection, and progress reporting,
	// so every Progress snapshot is consistent and Done never goes backwards.
	var (
		mu           sync.Mutex
		done, failed int
		written      int
		ch           = make(chan unit)
		wg           sync.WaitGroup
	)
	finish := func(u unit, wroteWords bool, err error) {
		mu.Lock()
		defer mu.Unlock()
		done++
		if err != nil {
			failed++
			res.Errors = append(res.Errors, LineError{LineID: u.lineID, RelPath: u.relPath, Err: err})
		} else {
			written++
			res.WrittenFiles = append(res.WrittenFiles, u.relPath)
			if wroteWords {
				res.WrittenFiles = append(res.WrittenFiles,
					u.relPath[:len(u.relPath)-len(filepath.Ext(u.relPath))]+".words.json")
			}
			// Record only what actually landed: a zero-word response writes no
			// sidecar, and the entry must not claim one.
			u.ent.Words = u.ent.Words && wroteWords
			man.set(u.relPath, u.ent)
			// Flush periodically so a crash or kill mid-batch doesn't lose the
			// record of audio already paid for. Best-effort — the save on the
			// way out of Run is authoritative.
			if written%manifestFlushEvery == 0 {
				_ = man.save(r.Options.OutputDir)
			}
		}
		r.report(Progress{Total: total, Done: done, Failed: failed})
	}

	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range ch {
				if ctx.Err() != nil {
					return
				}
				wroteWords, err := r.renderOne(ctx, u)
				finish(u, wroteWords, err)
			}
		}()
	}

	go func() {
		defer close(ch)
		for _, u := range units {
			select {
			case <-ctx.Done():
				return
			case ch <- u:
			}
		}
	}()

	wg.Wait()

	mu.Lock()
	res.Written = written
	res.Failed += failed
	mu.Unlock()
	return ctx.Err()
}

// concurrency resolves how many requests to run in parallel. An explicit
// Options.Concurrency wins; otherwise it asks the account what tier it is on and
// uses that tier's full documented cap. If the account can't be reached or its
// tier isn't recognized, it falls back to a conservative value rather than
// failing the run — worst case the batch is slower, never broken.
func (r *Runner) concurrency(ctx context.Context) int {
	if r.Options.Concurrency > 0 {
		return r.Options.Concurrency
	}
	sub, err := r.Client.Subscription(ctx)
	if err != nil || sub == nil {
		return synth.ConservativeConcurrency
	}
	n, known := synth.MaxConcurrency(sub.Tier)
	if !known {
		return synth.ConservativeConcurrency
	}
	return n
}

// renderOne synthesizes a single unit and writes it (plus word timings, when
// requested) to disk. It reports whether a timings sidecar was written.
func (r *Runner) renderOne(ctx context.Context, u unit) (wroteWords bool, err error) {
	// Defense in depth behind the plan-time checks — and strictly BEFORE the
	// paid API call, so a hostile path can never cost money on every re-run.
	base := filepath.Clean(r.Options.OutputDir)
	full := filepath.Join(base, filepath.FromSlash(u.relPath))
	if rel, err := filepath.Rel(base, full); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, fmt.Errorf("output path %q escapes the output folder", u.relPath)
	}

	var locators []synth.DictionaryLocator
	if r.Pronunciations.Active() {
		locators = []synth.DictionaryLocator{{
			ID:        r.Pronunciations.DictionaryID,
			VersionID: r.Pronunciations.VersionID,
		}}
	}
	out, err := r.Client.Synthesize(ctx, synth.Request{
		VoiceID:            u.voiceID,
		Text:               u.text,
		ModelID:            u.model,
		Format:             r.Options.Format,
		WithTimestamps:     r.Options.WithTimestamps,
		VoiceSettings:      u.settings,
		DictionaryLocators: locators,
	})
	if err != nil {
		return false, err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(full, out.Audio, 0o644); err != nil {
		return false, err
	}
	if r.Options.WithTimestamps && len(out.Words) > 0 {
		if err := writeTimings(full, out.Words); err != nil {
			return false, err
		}
		wroteWords = true
	}
	return wroteWords, nil
}

func (r *Runner) report(p Progress) {
	if r.OnProgress != nil {
		r.OnProgress(p)
	}
}

// writeTimings writes word timings beside the audio as "<base>.words.json".
func writeTimings(audioPath string, words []synth.WordTiming) error {
	type word struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	}
	ws := make([]word, 0, len(words))
	for _, w := range words {
		ws = append(ws, word{Word: w.Word, Start: w.Start, End: w.End})
	}
	data, err := json.MarshalIndent(struct {
		Words []word `json:"words"`
	}{ws}, "", "  ")
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	return os.WriteFile(base+".words.json", append(data, '\n'), 0o644)
}

func hashText(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// extForFormat maps an ElevenLabs output_format to a file extension.
// TODO (phase 2): wrap pcm_* in a RIFF/WAVE header and emit ".wav".
func extForFormat(f synth.AudioFormat) string {
	s := string(f)
	switch {
	case s == "" || strings.HasPrefix(s, "mp3"):
		return ".mp3"
	case strings.HasPrefix(s, "pcm"):
		return ".pcm"
	case strings.HasPrefix(s, "ulaw"):
		return ".ulaw"
	case strings.HasPrefix(s, "opus"):
		return ".opus"
	default:
		return ".audio"
	}
}

// --- manifest ----------------------------------------------------------------

// manifest records, per output-relative path, everything its audio was
// generated from, so a re-run regenerates only what changed. v1 manifests
// stored a bare text-hash string per file; entry.UnmarshalJSON still accepts
// that, with the missing fields acting as wildcards, so upgrading doesn't
// regenerate (re-pay for) existing output folders.
type manifest struct {
	Files map[string]entry `json:"files"`

	mu sync.Mutex
}

// entry is one file's generation record.
type entry struct {
	Text     string `json:"text"`               // hash of the exact text sent
	Voice    string `json:"voice,omitempty"`    // ElevenLabs voice ID
	Model    string `json:"model,omitempty"`    // ElevenLabs model ID
	Format   string `json:"format,omitempty"`   // output_format
	Words    bool   `json:"words,omitempty"`    // word-timings sidecar written
	Settings string `json:"settings,omitempty"` // canonical effective voice settings
	Dict     string `json:"dict,omitempty"`     // pronunciation rules affecting this line
}

// UnmarshalJSON accepts both the current object form and the legacy v1 bare
// text-hash string.
func (e *entry) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*e = entry{Text: s}
		return nil
	}
	type plain entry
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*e = entry(p)
	return nil
}

func loadManifest(dir string) (*manifest, error) {
	m := &manifest{Files: map[string]entry{}}
	path := filepath.Join(dir, manifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, m); err != nil {
		// Silently rebuilding would re-pay for the whole batch — fail loudly
		// and let the user delete the file on purpose.
		return nil, fmt.Errorf("%s is corrupt (%v); delete it to rebuild from scratch (regenerates everything)", path, err)
	}
	if m.Files == nil {
		m.Files = map[string]entry{}
	}
	return m, nil
}

// upToDate reports whether rel is on disk and was generated from this exact
// text, voice, model, and format. Fields empty in the recorded entry (legacy
// v1 manifests) match anything. When timings are requested, the sidecar itself
// must be on disk — that's what grandfathers legacy runs that already produced
// sidecars, regenerates entries whose sidecar was deleted, and forces a
// regenerate when timings are newly requested.
func (m *manifest) upToDate(dir, rel string, want entry) bool {
	m.mu.Lock()
	rec, ok := m.Files[rel]
	m.mu.Unlock()
	if !ok || rec.Text != want.Text {
		return false
	}
	if rec.Voice != "" && rec.Voice != want.Voice {
		return false
	}
	if rec.Model != "" && rec.Model != want.Model {
		return false
	}
	if rec.Format != "" && rec.Format != want.Format {
		return false
	}
	// Settings compare exactly: "" is a real value ("none sent"), so adding or
	// changing knobs regenerates, including against pre-settings manifests.
	if rec.Settings != want.Settings {
		return false
	}
	if rec.Dict != want.Dict {
		return false
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if want.Words {
		base := strings.TrimSuffix(full, filepath.Ext(full))
		if _, err := os.Stat(base + ".words.json"); err != nil {
			return false
		}
	}
	_, err := os.Stat(full)
	return err == nil
}

func (m *manifest) set(rel string, e entry) {
	m.mu.Lock()
	m.Files[rel] = e
	m.mu.Unlock()
}

func (m *manifest) remove(rel string) {
	m.mu.Lock()
	delete(m.Files, rel)
	m.mu.Unlock()
}

// save writes atomically (temp + rename) so a crash mid-write can't leave a
// truncated manifest that would then fail to load.
func (m *manifest) save(dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, manifestName)
	// A unique temp name so two processes generating into one folder (CLI and
	// app at once) can't interleave writes and rename a torn file into place.
	tmp, err := os.CreateTemp(dir, manifestName+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	_ = os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), path)
}
