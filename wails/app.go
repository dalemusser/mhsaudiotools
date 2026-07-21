// Package main is the Wails desktop shell. Every bound method on App is a thin
// adapter: it validates input, calls engine/, and shapes the result for the UI.
// No dialog or audio logic lives here — that all belongs to the engine, which
// the CLI drives through the same calls.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/dalemusser/aigenaudiotools/engine/job"
	"github.com/dalemusser/aigenaudiotools/engine/keys"
	"github.com/dalemusser/aigenaudiotools/engine/output"
	"github.com/dalemusser/aigenaudiotools/engine/source"
	"github.com/dalemusser/aigenaudiotools/engine/synth"
	"github.com/dalemusser/aigenaudiotools/engine/text"
	"github.com/dalemusser/aigenaudiotools/engine/voice"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the bound object; its exported methods are callable from JS.
type App struct {
	ctx context.Context

	mu     sync.Mutex
	cancel context.CancelFunc // non-nil while a run is in flight
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// --- settings ---------------------------------------------------------------

// Settings is what the UI needs to know at startup.
type Settings struct {
	HasKey    bool   `json:"hasKey"`
	KeySource string `json:"keySource"`
}

// GetSettings reports whether a key is already available and where it came from,
// so the UI doesn't nag someone who already has one configured.
func (a *App) GetSettings() Settings {
	if k := strings.TrimSpace(os.Getenv(keys.EnvVar)); k != "" {
		return Settings{HasKey: true, KeySource: "$" + keys.EnvVar}
	}
	if path, err := keys.HomePath(); err == nil {
		if k, err := keys.ReadFile(path); err == nil && k != "" {
			return Settings{HasKey: true, KeySource: path}
		}
	}
	return Settings{}
}

// SaveKey stores the key in the user's home directory (owner-only) and returns
// the path, so it's visible where it went rather than hidden.
func (a *App) SaveKey(key string) (string, error) { return keys.SaveToHome(key) }

// AccountInfo describes the account behind the key.
type AccountInfo struct {
	Tier           string `json:"tier"`
	TierKnown      bool   `json:"tierKnown"`
	MaxConcurrency int    `json:"maxConcurrency"`
	CharacterCount int    `json:"characterCount"`
	CharacterLimit int    `json:"characterLimit"`
}

// GetAccount reports the tier and the cap the UI's parallel slider maxes out at.
func (a *App) GetAccount() (*AccountInfo, error) {
	key, err := keys.Resolve()
	if err != nil {
		return nil, err
	}
	sub, err := synth.NewElevenLabs(key).Subscription(a.ctx)
	if err != nil {
		return nil, err
	}
	n, known := synth.MaxConcurrency(sub.Tier)
	return &AccountInfo{
		Tier:           sub.Tier,
		TierKnown:      known,
		MaxConcurrency: n,
		CharacterCount: sub.CharacterCount,
		CharacterLimit: sub.CharacterLimit,
	}, nil
}

// --- file pickers -----------------------------------------------------------

// PickSourceFile opens a dialog for a dialog source (an export CSV or a script).
func (a *App) PickSourceFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a dialog source",
		Filters: []runtime.FileFilter{
			{DisplayName: "Dialog sources (*.csv, *.txt)", Pattern: "*.csv;*.txt"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// PickVoicesFile opens a dialog for a voices.json or a VoiceAssignments.csv.
func (a *App) PickVoicesFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose voices.json or VoiceAssignments.csv",
		Filters: []runtime.FileFilter{
			{DisplayName: "Voice config (*.json, *.csv)", Pattern: "*.json;*.csv"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// PickOutputDir opens a folder picker for the output location.
func (a *App) PickOutputDir() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose the output folder",
	})
}

// --- voices -----------------------------------------------------------------

// VoicesInfo summarizes a loaded voice config for display.
type VoicesInfo struct {
	Path        string             `json:"path"`
	Assignments []voice.Assignment `json:"assignments"`
	PlayerSlots []voice.Slot       `json:"playerSlots"`
	Problems    []string           `json:"problems"`
}

// LoadVoices reads a voices.json or a VoiceAssignments.csv for display.
func (a *App) LoadVoices(path string) (*VoicesInfo, error) {
	cfg, err := loadVoiceConfig(path)
	if err != nil {
		return nil, err
	}
	return &VoicesInfo{
		Path:        path,
		Assignments: cfg.Assignments,
		PlayerSlots: cfg.SortedSlots(),
		Problems:    cfg.Validate(),
	}, nil
}

// ImportVoicesCSV folds a VoiceAssignments.csv into a voices.json, preserving
// existing player-slot bindings (engine/voice.Config.MergeFrom) so a reordered
// CSV can never move a voice to a different Player<N> folder.
func (a *App) ImportVoicesCSV(csvPath, outPath string) (*VoicesInfo, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	imported, err := voice.LoadAssignmentsCSV(f)
	if err != nil {
		return nil, err
	}
	if outPath == "" {
		outPath = filepath.Join(filepath.Dir(csvPath), "voices.json")
	}

	cfg, err := voice.LoadConfig(outPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		cfg = imported // first import: the CSV establishes the slot numbering
	} else {
		cfg.MergeFrom(imported)
	}
	if err := cfg.Save(outPath); err != nil {
		return nil, err
	}
	return &VoicesInfo{
		Path:        outPath,
		Assignments: cfg.Assignments,
		PlayerSlots: cfg.SortedSlots(),
		Problems:    cfg.Validate(),
	}, nil
}

// FetchVoices lists the account's voices so the UI can offer a picker instead of
// asking anyone to paste a voice ID.
func (a *App) FetchVoices() ([]synth.Voice, error) {
	key, err := keys.Resolve()
	if err != nil {
		return nil, err
	}
	voices, err := synth.NewElevenLabs(key).Voices(a.ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(voices, func(i, j int) bool { return voices[i].Name < voices[j].Name })
	return voices, nil
}

// --- generate ---------------------------------------------------------------

// Request is everything the UI collects for a run.
type Request struct {
	SourcePath     string `json:"sourcePath"`
	SourceFormat   string `json:"sourceFormat"` // "auto", "dbexport", "simplescript"
	VoicesPath     string `json:"voicesPath"`
	OutputDir      string `json:"outputDir"`
	Layout         string `json:"layout"`
	Format         string `json:"format"`
	Timestamps     bool   `json:"timestamps"`
	Concurrency    int    `json:"concurrency"` // 0 = auto (the account's max)
	Force          bool   `json:"force"`
	Cleanup        bool   `json:"cleanup"`
	DefaultSpeaker string `json:"defaultSpeaker"`
}

// VoiceCount is a per-voice tally for the preview.
type VoiceCount struct {
	Voice string `json:"voice"`
	Count int    `json:"count"`
}

// SampleItem is one example row in the preview.
type SampleItem struct {
	RelPath string `json:"relPath"`
	Voice   string `json:"voice"`
	Text    string `json:"text"`
}

// Preview reports what a run would do. It makes no API calls and needs no key —
// the cheap sanity check before committing to thousands of paid requests.
type Preview struct {
	SourceFormat string       `json:"sourceFormat"`
	Lines        int          `json:"lines"`
	SkippedLines int          `json:"skippedLines"`
	Targets      int          `json:"targets"`
	ToGenerate   int          `json:"toGenerate"`
	UpToDate     int          `json:"upToDate"`
	Characters   int          `json:"characters"`
	PerVoice     []VoiceCount `json:"perVoice"`
	Samples      []SampleItem `json:"samples"`
	Problems     []string     `json:"problems"`
}

// Preview builds a dry-run plan for the request.
func (a *App) Preview(req Request) (*Preview, error) {
	runner, lines, srcName, err := a.build(req)
	if err != nil {
		return nil, err
	}
	plan, err := runner.Plan(lines)
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}
	for _, it := range plan.Items {
		if it.UpToDate {
			continue
		}
		name := it.VoiceName
		if name == "" {
			name = it.VoiceID
		}
		counts[name]++
	}
	perVoice := make([]VoiceCount, 0, len(counts))
	for v, c := range counts {
		perVoice = append(perVoice, VoiceCount{Voice: v, Count: c})
	}
	sort.Slice(perVoice, func(i, j int) bool { return perVoice[i].Count > perVoice[j].Count })

	samples := make([]SampleItem, 0, 8)
	for _, it := range plan.Items {
		if len(samples) == 8 {
			break
		}
		samples = append(samples, SampleItem{RelPath: it.RelPath, Voice: it.VoiceName, Text: it.Text})
	}

	problems := make([]string, 0, len(plan.Errors))
	for i, e := range plan.Errors {
		if i == 50 {
			problems = append(problems, fmt.Sprintf("… and %d more", len(plan.Errors)-50))
			break
		}
		problems = append(problems, e.Error())
	}

	return &Preview{
		SourceFormat: srcName,
		Lines:        plan.Lines,
		SkippedLines: plan.SkippedLines,
		Targets:      plan.Targets,
		ToGenerate:   plan.ToGenerate,
		UpToDate:     plan.UpToDate,
		Characters:   plan.Characters,
		PerVoice:     perVoice,
		Samples:      samples,
		Problems:     problems,
	}, nil
}

// RunSummary is the outcome of a finished run.
type RunSummary struct {
	Written      int      `json:"written"`
	UpToDate     int      `json:"upToDate"`
	SkippedLines int      `json:"skippedLines"`
	Failed       int      `json:"failed"`
	Problems     []string `json:"problems"`
	ManifestPath string   `json:"manifestPath"`
	Canceled     bool     `json:"canceled"`
}

// Generate runs the batch, emitting "progress" events as files complete. It
// returns a summary even when canceled, so the UI can report partial work.
func (a *App) Generate(req Request) (*RunSummary, error) {
	runner, lines, _, err := a.build(req)
	if err != nil {
		return nil, err
	}
	key, err := keys.Resolve()
	if err != nil {
		return nil, err
	}
	runner.Client = synth.NewElevenLabs(key)
	runner.OnProgress = func(p job.Progress) {
		runtime.EventsEmit(a.ctx, "progress", map[string]int{
			"total": p.Total, "done": p.Done, "failed": p.Failed,
		})
	}

	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("a generation run is already in progress")
	}
	a.cancel = cancel
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cancel = nil
		a.mu.Unlock()
		cancel()
	}()

	res, runErr := runner.Run(ctx, lines)
	if res == nil {
		return nil, runErr
	}

	sum := &RunSummary{
		Written:      res.Written,
		UpToDate:     res.SkippedFiles,
		SkippedLines: res.SkippedLines,
		Failed:       res.Failed,
		ManifestPath: res.ManifestPath,
		Canceled:     ctx.Err() != nil,
	}
	for i, e := range res.Errors {
		if i == 50 {
			sum.Problems = append(sum.Problems, fmt.Sprintf("… and %d more", len(res.Errors)-50))
			break
		}
		sum.Problems = append(sum.Problems, e.Error())
	}
	// Cancellation is a normal outcome, not an error: the manifest is saved, so
	// re-running resumes where it stopped.
	if runErr != nil && !sum.Canceled {
		return sum, runErr
	}
	return sum, nil
}

// Cancel stops a run in progress. Files already written stay, and the manifest
// means a re-run resumes rather than starting over.
func (a *App) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
	}
}

// RevealOutput opens the output folder in the system file manager — the payoff
// step, since the whole point is files on disk.
func (a *App) RevealOutput(dir string) error {
	if dir == "" {
		return fmt.Errorf("no output folder")
	}
	runtime.BrowserOpenURL(a.ctx, "file://"+dir)
	return nil
}

// --- shared wiring ----------------------------------------------------------

// build assembles the engine pieces for a request, validating as it goes so the
// UI shows a clear message rather than failing 3,000 files in.
func (a *App) build(req Request) (*job.Runner, []source.LineItem, string, error) {
	if req.SourcePath == "" {
		return nil, nil, "", fmt.Errorf("choose a dialog source file")
	}
	if req.VoicesPath == "" {
		return nil, nil, "", fmt.Errorf("choose a voices file")
	}
	if req.OutputDir == "" {
		return nil, nil, "", fmt.Errorf("choose an output folder")
	}

	lines, srcName, err := loadLines(req.SourcePath, req.SourceFormat)
	if err != nil {
		return nil, nil, "", err
	}
	if len(lines) == 0 {
		return nil, nil, "", fmt.Errorf("no dialog lines found in %s", filepath.Base(req.SourcePath))
	}

	cfg, err := loadVoiceConfig(req.VoicesPath)
	if err != nil {
		return nil, nil, "", err
	}
	if req.DefaultSpeaker != "" {
		as, ok := cfg.VoiceFor(req.DefaultSpeaker)
		if !ok {
			return nil, nil, "", fmt.Errorf("default speaker %q has no voice assignment", req.DefaultSpeaker)
		}
		cfg.Default = &as
	}

	var profile *text.Profile
	if req.Cleanup {
		profile = text.MHSProfile()
	}
	layout, err := pickLayout(req.Layout)
	if err != nil {
		return nil, nil, "", err
	}
	format := req.Format
	if format == "" {
		format = string(synth.MP3_44100_128)
	}

	return &job.Runner{
		Cleanup: profile,
		Voices:  cfg,
		Layout:  layout,
		Options: job.Options{
			OutputDir:      req.OutputDir,
			Format:         synth.AudioFormat(format),
			WithTimestamps: req.Timestamps,
			Concurrency:    req.Concurrency,
			Force:          req.Force,
		},
	}, lines, srcName, nil
}

func loadVoiceConfig(path string) (*voice.Config, error) {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return voice.LoadAssignmentsCSV(f)
	}
	return voice.LoadConfig(path)
}

func loadLines(path, adapter string) ([]source.LineItem, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	if adapter == "" || adapter == "auto" {
		return source.ParseAuto(f)
	}
	s, ok := source.Get(adapter)
	if !ok {
		return nil, "", fmt.Errorf("unknown source format %q", adapter)
	}
	lines, err := s.Parse(f)
	return lines, s.Name(), err
}

func pickLayout(name string) (output.Layout, error) {
	switch name {
	case "", "dialog-system":
		return output.DialogSystem{}, nil
	case "babylon-manifest":
		return output.BabylonManifest{}, nil
	default:
		return nil, fmt.Errorf("unknown layout %q", name)
	}
}
