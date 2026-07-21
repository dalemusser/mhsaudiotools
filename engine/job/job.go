// Package job runs a generation batch: it plans the work (clean text, resolve
// voices, expand player fan-out, drop what's already up to date), then executes
// it through a bounded worker pool with progress reporting and cancellation.
//
// TODO (phase 2): persist job records so runs survive an app restart and can be
// listed, resumed, and expired.
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
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dalemusser/mhsaudiotools/engine/output"
	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/synth"
	"github.com/dalemusser/mhsaudiotools/engine/text"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// manifestName records each written file's text hash, enabling idempotent
// re-runs (regenerate only what changed).
const manifestName = ".aigenaudio-manifest.json"

// Options configures a generation run.
type Options struct {
	OutputDir      string
	Format         synth.AudioFormat
	WithTimestamps bool

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
}

// Runner executes a generation run over normalized lines.
type Runner struct {
	Client  synth.Client
	Cleanup *text.Profile // optional; nil means no text cleanup
	Voices  *voice.Config
	Layout  output.Layout
	Options Options

	// OnProgress, if set, is called as units complete. It must be safe to call
	// from multiple goroutines and should not block.
	OnProgress func(Progress)
}

// unit is one audio file to synthesize and write.
type unit struct {
	lineID  string
	relPath string
	voiceID string
	text    string
	hash    string
}

// PlanItem is one audio file a run would produce.
type PlanItem struct {
	LineID    string
	RelPath   string
	VoiceID   string
	VoiceName string
	Text      string // the post-cleanup text that will actually be spoken
	UpToDate  bool   // already generated from this exact text; would be skipped
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
			lineID:  it.LineID,
			relPath: it.RelPath,
			voiceID: it.VoiceID,
			text:    it.Text,
			hash:    hashText(it.Text),
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
	return res, nil
}

// planWith works out what the run would produce: it cleans each line's text,
// drops lines that clean to nothing, resolves voicing (fanning player lines out
// across slots), and marks targets already up to date.
func (r *Runner) planWith(lines []source.LineItem, man *manifest) *PlanResult {
	ext := extForFormat(r.Options.Format)
	p := &PlanResult{Lines: len(lines)}

	for _, li := range lines {
		spoken := li.Text
		if r.Cleanup != nil {
			cleaned, err := r.Cleanup.Apply(li.Text)
			if err != nil {
				p.Errors = append(p.Errors, LineError{LineID: li.ID, Err: err})
				p.Failed++
				continue
			}
			spoken = cleaned
		}
		spoken = strings.TrimSpace(spoken)
		if spoken == "" {
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

		hash := hashText(spoken)
		for _, tg := range targets {
			p.Targets++
			item := PlanItem{
				LineID:    li.ID,
				RelPath:   tg.RelPath,
				VoiceID:   tg.VoiceID,
				VoiceName: tg.VoiceName,
				Text:      spoken,
			}
			if !r.Options.Force && man.upToDate(r.Options.OutputDir, tg.RelPath, hash) {
				item.UpToDate = true
				p.UpToDate++
			} else {
				p.ToGenerate++
				p.Characters += len(spoken)
			}
			p.Items = append(p.Items, item)
		}
	}
	return p
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

	var (
		mu           sync.Mutex // guards res.Errors
		done, failed int64
		written      int64
		ch           = make(chan unit)
		wg           sync.WaitGroup
	)

	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range ch {
				if ctx.Err() != nil {
					return
				}
				err := r.renderOne(ctx, u)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					mu.Lock()
					res.Errors = append(res.Errors, LineError{LineID: u.lineID, RelPath: u.relPath, Err: err})
					mu.Unlock()
				} else {
					atomic.AddInt64(&written, 1)
					man.set(u.relPath, u.hash)
				}
				r.report(Progress{
					Total:  total,
					Done:   int(atomic.AddInt64(&done, 1)),
					Failed: int(atomic.LoadInt64(&failed)),
				})
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

	res.Written = int(atomic.LoadInt64(&written))
	res.Failed += int(atomic.LoadInt64(&failed))
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
// requested) to disk.
func (r *Runner) renderOne(ctx context.Context, u unit) error {
	out, err := r.Client.Synthesize(ctx, synth.Request{
		VoiceID:        u.voiceID,
		Text:           u.text,
		Format:         r.Options.Format,
		WithTimestamps: r.Options.WithTimestamps,
	})
	if err != nil {
		return err
	}

	full := filepath.Join(r.Options.OutputDir, filepath.FromSlash(u.relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, out.Audio, 0o644); err != nil {
		return err
	}
	if r.Options.WithTimestamps && len(out.Words) > 0 {
		if err := writeTimings(full, out.Words); err != nil {
			return err
		}
	}
	return nil
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

// manifest maps an output-relative path to the hash of the text it was
// generated from, so a re-run regenerates only changed or missing files.
type manifest struct {
	Files map[string]string `json:"files"`

	mu sync.Mutex
}

func loadManifest(dir string) (*manifest, error) {
	m := &manifest{Files: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, m); err != nil {
		// A corrupt manifest shouldn't fail the run — rebuild it instead.
		return &manifest{Files: map[string]string{}}, nil
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, nil
}

// upToDate reports whether rel was generated from this exact text and is still
// on disk.
func (m *manifest) upToDate(dir, rel, hash string) bool {
	m.mu.Lock()
	recorded := m.Files[rel]
	m.mu.Unlock()
	if recorded != hash {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func (m *manifest) set(rel, hash string) {
	m.mu.Lock()
	m.Files[rel] = hash
	m.mu.Unlock()
}

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
	return os.WriteFile(filepath.Join(dir, manifestName), append(data, '\n'), 0o644)
}
