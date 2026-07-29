// Package main is the Wails desktop shell. Every bound method on App is a thin
// adapter: it validates input, calls engine/, and shapes the result for the UI.
// No dialog or audio logic lives here — that all belongs to the engine, which
// the CLI drives through the same calls.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dalemusser/mhsaudiotools/engine/emotion"
	"github.com/dalemusser/mhsaudiotools/engine/job"
	"github.com/dalemusser/mhsaudiotools/engine/keys"
	"github.com/dalemusser/mhsaudiotools/engine/output"
	"github.com/dalemusser/mhsaudiotools/engine/pron"
	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/synth"
	"github.com/dalemusser/mhsaudiotools/engine/text"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the bound object; its exported methods are callable from JS.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	cancel   context.CancelFunc // non-nil while a run is in flight
	runDone  chan struct{}      // closed when the in-flight Generate returns
	quitting bool               // a deferred (Windows) close is already in flight

	jobs *job.Store // run history; nil when the config dir is unavailable
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Job history is a convenience: if the config dir is unusable the app runs
	// without it rather than failing to start.
	if path, err := job.DefaultStorePath(); err == nil {
		a.jobs = job.OpenStore(path)
		// Anything still "running" belongs to a previous process that died —
		// surface it as interrupted so the UI can offer to resume.
		_, _ = a.jobs.MarkInterrupted()
	}
}

// beforeClose runs when the user closes the window. Wails never cancels the app
// context on close, so without this a mid-run quit would kill the process
// wherever the worker pool happened to be — losing the manifest record of audio
// already paid for. Cancel the run and wait (bounded) for Generate to finish
// saving before letting the window go.
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return false // our own deferred quit coming back around — let it close
	}
	cancel, done := a.cancel, a.runDone
	a.mu.Unlock()
	if cancel == nil {
		return false // nothing running — close normally
	}
	cancel()

	// On Windows this hook runs synchronously on the message loop, so blocking
	// here ghosts the window ("Not Responding"). Deny this close, finish the
	// drain off-thread, then quit programmatically. macOS and Linux invoke the
	// hook on its own goroutine, so blocking there is fine.
	if goruntime.GOOS == "windows" {
		go func() {
			select {
			case <-done:
			case <-time.After(15 * time.Second):
			}
			a.mu.Lock()
			a.quitting = true
			a.mu.Unlock()
			runtime.Quit(a.ctx)
		}()
		return true
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
	return false
}

// --- settings ---------------------------------------------------------------

// Settings is what the UI needs to know at startup.
type Settings struct {
	HasKey            bool   `json:"hasKey"`
	KeySource         string `json:"keySource"`
	KeychainAvailable bool   `json:"keychainAvailable"`
}

// GetSettings reports whether a key is already available and where it came from,
// so the UI doesn't nag someone who already has one configured.
func (a *App) GetSettings() Settings {
	src, found := keys.Source()
	return Settings{HasKey: found, KeySource: src, KeychainAvailable: keys.KeychainSupported()}
}

// SaveKey stores the key — in the macOS Keychain when asked (and available),
// else in the owner-only home dotfile — and returns where it went.
func (a *App) SaveKey(key string, useKeychain bool) (string, error) {
	if useKeychain && keys.KeychainSupported() {
		if err := keys.SaveToKeychain(key); err != nil {
			return "", err
		}
		return "the macOS Keychain", nil
	}
	return keys.SaveToHome(key)
}

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

// SaveVoices validates and writes an edited voice config, preserving any existing
// default voice (which is set outside this editor). A blank path opens a save
// dialog for a brand-new file. Returns the saved config's summary.
func (a *App) SaveVoices(path string, assignments []voice.Assignment, slots []voice.Slot) (*VoicesInfo, error) {
	if path == "" {
		p, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Save voices.json",
			DefaultFilename: "voices.json",
		})
		if err != nil || p == "" {
			return nil, err
		}
		path = p
	}
	cfg := &voice.Config{Assignments: assignments, PlayerSlots: slots}
	if existing, err := voice.LoadConfig(path); err == nil {
		cfg.Default = existing.Default // don't drop a default set elsewhere
	}
	if err := cfg.Save(path); err != nil {
		return nil, err
	}
	return &VoicesInfo{
		Path:        path,
		Assignments: cfg.Assignments,
		PlayerSlots: cfg.SortedSlots(),
		Problems:    cfg.Validate(),
	}, nil
}

// previewSample is spoken when previewing a voice with no custom text.
const previewSample = "Good morning, cadets. This is a voice preview."

// PreviewVoice synthesizes a short sample in the given voice and model, returning
// it as a base64 audio data URI the UI can play — so an artist can hear a voice
// (and audition v2 vs v3) before committing. An empty model uses v2.
func (a *App) PreviewVoice(voiceID, sample, model string, settings *voice.Settings) (string, error) {
	if voiceID == "" {
		return "", fmt.Errorf("pick a voice to preview")
	}
	if strings.TrimSpace(sample) == "" {
		sample = previewSample
	}
	key, err := keys.Resolve()
	if err != nil {
		return "", err
	}
	// The audition must sound like the run will: same settings resolution.
	var vs *synth.VoiceSettings
	if settings != nil {
		vs = &synth.VoiceSettings{
			Stability:       settings.Stability,
			SimilarityBoost: settings.SimilarityBoost,
			Style:           settings.Style,
			UseSpeakerBoost: settings.SpeakerBoost,
			Speed:           settings.Speed,
		}
	}
	res, err := synth.NewElevenLabs(key).Synthesize(a.ctx, synth.Request{
		VoiceID:       voiceID,
		Text:          sample,
		ModelID:       model,
		Format:        synth.MP3_44100_128,
		VoiceSettings: vs,
	})
	if err != nil {
		return "", err
	}
	return "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(res.Audio), nil
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

// --- cleanup profile --------------------------------------------------------

// CleanupInfo summarizes a cleanup profile for display.
type CleanupInfo struct {
	Path         string   `json:"path"` // empty for the built-in defaults
	Name         string   `json:"name"`
	Rules        int      `json:"rules"`
	RemoveCount  int      `json:"removeCount"`
	ReplaceCount int      `json:"replaceCount"`
	Problems     []string `json:"problems"`
	BuiltIn      bool     `json:"builtIn"`
}

func cleanupInfo(path string, p *text.Profile, builtIn bool) *CleanupInfo {
	info := &CleanupInfo{Path: path, Name: p.Name, Rules: len(p.Rules), BuiltIn: builtIn}
	for _, r := range p.Rules {
		if r.Op == text.OpRemove {
			info.RemoveCount++
		} else {
			info.ReplaceCount++
		}
	}
	if err := p.Validate(); err != nil {
		info.Problems = append(info.Problems, err.Error())
	}
	return info
}

// DefaultCleanup summarizes the built-in MHS cleanup profile.
func (a *App) DefaultCleanup() *CleanupInfo {
	return cleanupInfo("", text.MHSProfile(), true)
}

// PickCleanupFile opens a dialog for a cleanup.json and returns its summary.
func (a *App) PickCleanupFile() (*CleanupInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Choose a cleanup.json",
		Filters: []runtime.FileFilter{{DisplayName: "Cleanup profile (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	p, err := text.LoadProfile(path)
	if err != nil {
		return nil, err
	}
	return cleanupInfo(path, p, false), nil
}

// RuleDTO / CleanupDTO mirror text.Rule/Profile with plain-string enums, so the
// JS editor exchanges rules as readable "literal"/"regex", "remove"/"replace"
// rather than the marshaled enum ints.
type RuleDTO struct {
	Kind string `json:"kind"` // "literal" | "regex"
	Op   string `json:"op"`   // "remove" | "replace"
	From string `json:"from"`
	To   string `json:"to"`
	Note string `json:"note"`
}

type CleanupDTO struct {
	Name  string    `json:"name"`
	Rules []RuleDTO `json:"rules"`
}

func dtoFromProfile(p *text.Profile) CleanupDTO {
	dto := CleanupDTO{Name: p.Name, Rules: make([]RuleDTO, 0, len(p.Rules))}
	for _, r := range p.Rules {
		dto.Rules = append(dto.Rules, RuleDTO{
			Kind: r.Kind.String(), Op: r.Op.String(), From: r.From, To: r.To, Note: r.Note,
		})
	}
	return dto
}

func profileFromDTO(dto CleanupDTO) (*text.Profile, error) {
	p := &text.Profile{Name: dto.Name}
	for i, d := range dto.Rules {
		var k text.RuleKind
		switch d.Kind {
		case "literal":
			k = text.RuleLiteral
		case "regex":
			k = text.RuleRegex
		default:
			return nil, fmt.Errorf("rule %d: unknown kind %q", i+1, d.Kind)
		}
		var op text.RuleOp
		switch d.Op {
		case "remove":
			op = text.OpRemove
		case "replace":
			op = text.OpReplace
		default:
			return nil, fmt.Errorf("rule %d: unknown op %q", i+1, d.Op)
		}
		p.Rules = append(p.Rules, text.Rule{Kind: k, Op: op, From: d.From, To: d.To, Note: d.Note})
	}
	return p, nil
}

// CleanupRules returns the editable rules for a profile — built-in MHS defaults
// when path is empty, else the file's rules.
func (a *App) CleanupRules(path string) (*CleanupDTO, error) {
	if path == "" {
		dto := dtoFromProfile(text.MHSProfile())
		return &dto, nil
	}
	p, err := text.LoadProfile(path)
	if err != nil {
		return nil, err
	}
	dto := dtoFromProfile(p)
	return &dto, nil
}

// TestCleanup applies the given rules to a sample line, so the editor can show
// the effect live (and surface a bad regex immediately).
func (a *App) TestCleanup(rules []RuleDTO, sample string) (string, error) {
	p, err := profileFromDTO(CleanupDTO{Rules: rules})
	if err != nil {
		return "", err
	}
	return p.Apply(sample)
}

// ScanForCleanup scans the chosen dialog source for markup the given rules don't
// remove, so the editor can suggest new remove rules.
func (a *App) ScanForCleanup(sourcePath, sourceFormat string, rules []RuleDTO) ([]text.Suggestion, error) {
	if sourcePath == "" {
		return nil, fmt.Errorf("choose a dialog source (step 1) first, so there's something to scan")
	}
	lines, _, err := loadLines(sourcePath, sourceFormat)
	if err != nil {
		return nil, err
	}
	p, err := profileFromDTO(CleanupDTO{Rules: rules})
	if err != nil {
		return nil, err
	}
	texts := make([]string, len(lines))
	for i, li := range lines {
		texts[i] = li.Text
	}
	return text.Suggest(texts, p)
}

// SaveCleanup validates and writes the edited rules; a blank path opens a save
// dialog. Returns the saved profile's summary.
func (a *App) SaveCleanup(path string, dto CleanupDTO) (*CleanupInfo, error) {
	p, err := profileFromDTO(dto)
	if err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if path == "" {
		sp, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Save cleanup.json",
			DefaultFilename: "cleanup.json",
		})
		if err != nil || sp == "" {
			return nil, err
		}
		path = sp
	}
	if err := p.Save(path); err != nil {
		return nil, err
	}
	return cleanupInfo(path, p, false), nil
}

// ExportDefaultCleanup writes the built-in MHS rules to a file the team can edit
// and share (the one shared cleanup.json), then selects it. Returns its summary.
func (a *App) ExportDefaultCleanup() (*CleanupInfo, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save cleanup.json (MHS defaults, to edit and share)",
		DefaultFilename: "cleanup.json",
	})
	if err != nil || path == "" {
		return nil, err
	}
	p := text.MHSProfile()
	if err := p.Save(path); err != nil {
		return nil, err
	}
	return cleanupInfo(path, p, false), nil
}

// --- emotion tag map --------------------------------------------------------

// EmotionRuleDTO is one direction→tag mapping for the editor.
type EmotionRuleDTO struct {
	Phrase string `json:"phrase"` // e.g. "sighs"
	Tag    string `json:"tag"`    // e.g. "[sighs]"
}

// EmotionDTO mirrors emotion.Map as an ordered list for editing.
type EmotionDTO struct {
	Name   string           `json:"name"`
	Rules  []EmotionRuleDTO `json:"rules"`
	Ignore []string         `json:"ignore"`
}

// EmotionInfo summarizes an emotion map for display.
type EmotionInfo struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Rules       int    `json:"rules"`
	IgnoreCount int    `json:"ignoreCount"`
	BuiltIn     bool   `json:"builtIn"`
}

func emotionDTOFromMap(m *emotion.Map) EmotionDTO {
	dto := EmotionDTO{Name: m.Name, Ignore: append([]string(nil), m.Ignore...)}
	keys := make([]string, 0, len(m.Tags))
	for k := range m.Tags {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable display order
	for _, k := range keys {
		dto.Rules = append(dto.Rules, EmotionRuleDTO{Phrase: k, Tag: m.Tags[k]})
	}
	return dto
}

func mapFromEmotionDTO(dto EmotionDTO) *emotion.Map {
	m := &emotion.Map{Name: dto.Name, Tags: map[string]string{}, Ignore: dto.Ignore}
	for _, r := range dto.Rules {
		phrase := strings.ToLower(strings.TrimSpace(r.Phrase))
		tag := strings.TrimSpace(r.Tag)
		if phrase == "" || tag == "" {
			continue
		}
		m.Tags[phrase] = tag
	}
	return m
}

func emotionInfo(path string, m *emotion.Map, builtIn bool) *EmotionInfo {
	return &EmotionInfo{Path: path, Name: m.Name, Rules: len(m.Tags), IgnoreCount: len(m.Ignore), BuiltIn: builtIn}
}

// DefaultEmotion summarizes the built-in emotion map.
func (a *App) DefaultEmotion() *EmotionInfo { return emotionInfo("", emotion.DefaultMap(), true) }

// EmotionRules returns the editable direction→tag rows (built-in defaults if
// path is empty).
func (a *App) EmotionRules(path string) (*EmotionDTO, error) {
	if path == "" {
		dto := emotionDTOFromMap(emotion.DefaultMap())
		return &dto, nil
	}
	m, err := emotion.LoadMap(path)
	if err != nil {
		return nil, err
	}
	dto := emotionDTOFromMap(m)
	return &dto, nil
}

// TestEmotion shows what a sample line becomes on v3: it extracts the
// parentheticals and applies the map's tags.
func (a *App) TestEmotion(dto EmotionDTO, sample string) (string, error) {
	m := mapFromEmotionDTO(dto)
	clean, dirs := emotion.Extract(sample)
	return m.Apply(clean, dirs), nil
}

// PickEmotionFile opens a dialog for an emotions.json and returns its summary.
func (a *App) PickEmotionFile() (*EmotionInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Choose an emotions.json",
		Filters: []runtime.FileFilter{{DisplayName: "Emotion map (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	m, err := emotion.LoadMap(path)
	if err != nil {
		return nil, err
	}
	return emotionInfo(path, m, false), nil
}

// --- pronunciations ----------------------------------------------------------

// PronRuleDTO is one written→spoken alias for the editor.
type PronRuleDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// PronDTO mirrors pron.Set's rules as an ordered list for editing.
type PronDTO struct {
	Name  string        `json:"name"`
	Rules []PronRuleDTO `json:"rules"`
}

// PronInfo summarizes the pronunciation set for display.
type PronInfo struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Rules     int    `json:"rules"`
	Published bool   `json:"published"`
}

func pronInfo(path string, s *pron.Set) *PronInfo {
	return &PronInfo{Path: path, Name: s.Name, Rules: len(s.Rules), Published: !s.NeedsPublish()}
}

func pronSetPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return pron.DefaultPath()
}

// PronunciationInfo summarizes the set at path ("" = the shared per-user file).
func (a *App) PronunciationInfo(path string) (*PronInfo, error) {
	p, err := pronSetPath(path)
	if err != nil {
		return nil, err
	}
	s, err := pron.LoadOrDefault(p)
	if err != nil {
		return nil, err
	}
	return pronInfo(p, s), nil
}

// PronunciationRules returns the editable written→spoken rows, sorted.
func (a *App) PronunciationRules(path string) (*PronDTO, error) {
	p, err := pronSetPath(path)
	if err != nil {
		return nil, err
	}
	s, err := pron.LoadOrDefault(p)
	if err != nil {
		return nil, err
	}
	dto := &PronDTO{Name: s.Name}
	for _, r := range s.SortedRules() {
		dto.Rules = append(dto.Rules, PronRuleDTO{From: r.From, To: r.To})
	}
	return dto, nil
}

// SavePronunciations writes the rows to the file ("" = the shared per-user
// file), preserving the publish state so drift is detected and the dictionary
// is republished on the next run.
func (a *App) SavePronunciations(path string, dto PronDTO) (*PronInfo, error) {
	p, err := pronSetPath(path)
	if err != nil {
		return nil, err
	}
	s, err := pron.LoadOrDefault(p)
	if err != nil {
		return nil, err
	}
	s.Name = strings.TrimSpace(dto.Name)
	if s.Name == "" {
		s.Name = "mhs-pronunciations"
	}
	s.Rules = map[string]string{}
	for _, r := range dto.Rules {
		from, to := strings.TrimSpace(r.From), strings.TrimSpace(r.To)
		if from == "" || to == "" {
			continue
		}
		s.Rules[from] = to
	}
	if err := s.Save(p); err != nil {
		return nil, err
	}
	return pronInfo(p, s), nil
}

// TestPronunciations shows what a sample line will sound like under the rules
// (client-side approximation of the server's alias matching).
func (a *App) TestPronunciations(dto PronDTO, sample string) (string, error) {
	out := sample
	for _, r := range dto.Rules {
		if r.From != "" && r.To != "" {
			out = strings.ReplaceAll(out, r.From, r.To)
		}
	}
	return out, nil
}

// PickPronunciationsFile chooses a custom pronunciations.json.
func (a *App) PickPronunciationsFile() (*PronInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Choose a pronunciations.json",
		Filters: []runtime.FileFilter{{DisplayName: "Pronunciations (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	s, err := pron.Load(path)
	if err != nil {
		return nil, err
	}
	return pronInfo(path, s), nil
}

// OverridesInfo summarizes a picked voice-overrides file.
type OverridesInfo struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// PickVoiceOverridesFile opens a dialog for a voice-overrides.json (per-line
// delivery tweaks keyed by line ID) and validates it before accepting.
func (a *App) PickVoiceOverridesFile() (*OverridesInfo, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Choose a voice-overrides.json",
		Filters: []runtime.FileFilter{{DisplayName: "Voice overrides (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	o, err := voice.LoadOverrides(path)
	if err != nil {
		return nil, err
	}
	return &OverridesInfo{Path: path, Count: len(o)}, nil
}

// ExportDefaultEmotion writes the built-in emotion map to a file to edit and
// share, then selects it.
func (a *App) ExportDefaultEmotion() (*EmotionInfo, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save emotions.json (defaults, to edit and share)",
		DefaultFilename: "emotions.json",
	})
	if err != nil || path == "" {
		return nil, err
	}
	m := emotion.DefaultMap()
	if err := m.Save(path); err != nil {
		return nil, err
	}
	return emotionInfo(path, m, false), nil
}

// SaveEmotion writes the edited map; a blank path opens a save dialog.
func (a *App) SaveEmotion(path string, dto EmotionDTO) (*EmotionInfo, error) {
	m := mapFromEmotionDTO(dto)
	if path == "" {
		sp, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Save emotions.json",
			DefaultFilename: "emotions.json",
		})
		if err != nil || sp == "" {
			return nil, err
		}
		path = sp
	}
	if err := m.Save(path); err != nil {
		return nil, err
	}
	return emotionInfo(path, m, false), nil
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
	CleanupPath    string `json:"cleanupPath"` // custom cleanup.json; empty = built-in MHS defaults
	Emotion        bool   `json:"emotion"`     // apply v3 audio tags to v3 voices
	EmotionPath    string `json:"emotionPath"` // custom emotions.json; empty = built-in defaults
	DefaultSpeaker string `json:"defaultSpeaker"`

	// VoiceOverridesPath is an optional voice-overrides.json: per-line delivery
	// tweaks (stability/style/speed) keyed by line ID.
	VoiceOverridesPath string `json:"voiceOverridesPath"`

	// Pronunciations applies the server-side pronunciation dictionary (text
	// keeps the writers' spelling; timings/captions align to it). Path "" uses
	// the shared per-user pronunciations file.
	Pronunciations     bool   `json:"pronunciations"`
	PronunciationsPath string `json:"pronunciationsPath"`
}

// pronPathFor resolves which pronunciations file a request uses.
func pronPathFor(req Request) string {
	if req.PronunciationsPath != "" {
		return req.PronunciationsPath
	}
	p, err := pron.DefaultPath()
	if err != nil {
		return ""
	}
	return p
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
	Orphans      []string     `json:"orphans"` // files from lines removed from the source
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
		if it.UpToDate {
			continue // show what this run will actually generate, like the tally above
		}
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
		Orphans:      plan.Orphans,
	}, nil
}

// PruneOrphans deletes files for lines no longer in the source — the engine
// refuses when the plan has problems and only touches manifest-known files.
// Needs no API key.
func (a *App) PruneOrphans(req Request) ([]string, error) {
	runner, lines, _, err := a.build(req)
	if err != nil {
		return nil, err
	}
	return runner.PruneOrphans(lines)
}

// ExportChangedFiles copies a finished run's written files into a folder the
// user picks — the "changed files only" set to import into the game project,
// so Perforce/Unity only sees what actually changed.
func (a *App) ExportChangedFiles(outputDir string, relPaths []string) (string, error) {
	if len(relPaths) == 0 {
		return "", fmt.Errorf("that run wrote no files")
	}
	dst, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder for the changed files",
	})
	if err != nil || dst == "" {
		return "", err
	}
	if err := job.CopyFiles(outputDir, dst, relPaths); err != nil {
		return "", err
	}
	return dst, nil
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

	// ChangedFiles are the output-relative paths this run wrote — what
	// "Copy changed files…" exports for game-project import.
	ChangedFiles []string `json:"changedFiles"`
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
	client := synth.NewElevenLabs(key)
	runner.Client = client

	// The dictionary must exist server-side before any request references it.
	if runner.Pronunciations.Active() && runner.Pronunciations.NeedsPublish() {
		if err := pron.Publish(a.ctx, client, runner.Pronunciations); err != nil {
			return nil, err
		}
		if p := pronPathFor(req); p != "" {
			if err := runner.Pronunciations.Save(p); err != nil {
				return nil, fmt.Errorf("saving pronunciations state: %w", err)
			}
		}
	}

	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("a generation run is already in progress")
	}
	a.cancel = cancel
	a.runDone = done
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cancel = nil
		a.runDone = nil
		a.mu.Unlock()
		cancel()
		close(done)
	}()

	// Record the run in the shared history so it survives a restart: created as
	// "running", updated with throttled progress (so an interrupted record
	// shows how far it got), finalized below. The stored Request is what Resume
	// replays.
	var rec job.Record
	if a.jobs != nil {
		reqJSON, _ := json.Marshal(req)
		rec = job.Record{
			ID: job.NewID(time.Now()), Created: time.Now(),
			Status: job.StatusRunning, Shell: "app",
			Source: req.SourcePath, OutputDir: req.OutputDir, Layout: req.Layout,
			Request: reqJSON,
		}
		_ = a.jobs.Put(rec)
	}
	var lastPersist time.Time
	runner.OnProgress = func(p job.Progress) {
		runtime.EventsEmit(a.ctx, "progress", map[string]int{
			"total": p.Total, "done": p.Done, "failed": p.Failed,
		})
		// OnProgress is serialized by the runner, so this is race-free.
		if a.jobs != nil && time.Since(lastPersist) > 5*time.Second {
			lastPersist = time.Now()
			rec.Targets, rec.Done, rec.Failed = p.Total, p.Done, p.Failed
			_ = a.jobs.Put(rec)
		}
	}

	res, runErr := runner.Run(ctx, lines)
	if res == nil {
		if a.jobs != nil {
			rec.Status, rec.Error = job.StatusFailed, runErr.Error()
			_ = a.jobs.Put(rec)
		}
		return nil, runErr
	}

	// Cancellation is what the run actually returned, not the ctx state at this
	// instant — a Cancel click racing normal completion (or a real failure)
	// must not relabel the run and swallow its error.
	canceled := errors.Is(runErr, context.Canceled) || (runErr == nil && ctx.Err() != nil)

	if a.jobs != nil {
		rec.Targets, rec.Written, rec.Failed = res.Targets, res.Written, res.Failed
		rec.Done = res.Written + res.Failed
		switch {
		case canceled:
			rec.Status = job.StatusCanceled
		case runErr != nil:
			rec.Status, rec.Error = job.StatusFailed, runErr.Error()
		default:
			rec.Status = job.StatusCompleted
		}
		_ = a.jobs.Put(rec)
	}
	sum := &RunSummary{
		Written:      res.Written,
		UpToDate:     res.SkippedFiles,
		SkippedLines: res.SkippedLines,
		Failed:       res.Failed,
		ManifestPath: res.ManifestPath,
		Canceled:     canceled,
		ChangedFiles: res.WrittenFiles,
	}
	for i, e := range res.Errors {
		if i == 50 {
			sum.Problems = append(sum.Problems, fmt.Sprintf("… and %d more", len(res.Errors)-50))
			break
		}
		sum.Problems = append(sum.Problems, e.Error())
	}
	// Cancellation is a normal outcome, not an error: the manifest is saved, so
	// re-running resumes where it stopped. Other errors ride in Problems —
	// Wails delivers a result OR an error, never both, and the counts matter
	// even when the final manifest save failed.
	if runErr != nil && !canceled {
		sum.Problems = append([]string{"run error: " + runErr.Error()}, sum.Problems...)
	}
	return sum, nil
}

// ListJobs returns the run history, newest first. Records carry the original
// Request, which the frontend replays to resume a job.
func (a *App) ListJobs() ([]job.Record, error) {
	if a.jobs == nil {
		return nil, nil
	}
	return a.jobs.List()
}

// DeleteJob removes one record from the history.
func (a *App) DeleteJob(id string) error {
	if a.jobs == nil {
		return nil
	}
	return a.jobs.Delete(id)
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

// OpenDocs opens the app's documentation (docs/USAGE.md on GitHub) in the
// default browser. https URLs are fine through BrowserOpenURL — it is only the
// file:// scheme it rejects (see RevealOutput).
func (a *App) OpenDocs() {
	runtime.BrowserOpenURL(a.ctx, "https://github.com/dalemusser/mhsaudiotools/blob/main/docs/USAGE.md")
}

// RevealOutput opens the output folder in the system file manager — the payoff
// step, since the whole point is files on disk. This execs the platform opener
// directly: Wails' BrowserOpenURL rejects the file:// scheme outright, so it
// can never open a folder.
func (a *App) RevealOutput(dir string) error {
	if dir == "" {
		return fmt.Errorf("no output folder")
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("output folder: %w", err)
	}
	if goruntime.GOOS == "windows" {
		// explorer exits nonzero even on success, so start it detached and
		// don't judge the exit code.
		cmd := exec.Command("explorer", dir)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("opening file manager: %w", err)
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	// open/xdg-open hand off and exit immediately, and their exit codes are
	// meaningful (e.g. xdg-open with no file manager) — run them to completion
	// so the failure reaches the UI instead of a silently dead button.
	opener := "xdg-open"
	if goruntime.GOOS == "darwin" {
		opener = "open"
	}
	if out, err := exec.Command(opener, dir).CombinedOutput(); err != nil {
		return fmt.Errorf("opening file manager: %v (%s)", err, strings.TrimSpace(string(out)))
	}
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
		if req.CleanupPath != "" {
			p, err := text.LoadProfile(req.CleanupPath)
			if err != nil {
				return nil, nil, "", fmt.Errorf("cleanup profile: %w", err)
			}
			profile = p
		} else {
			profile = text.MHSProfile()
		}
	}

	var emo *emotion.Map
	if req.Emotion {
		if req.EmotionPath != "" {
			m, err := emotion.LoadMap(req.EmotionPath)
			if err != nil {
				return nil, nil, "", fmt.Errorf("emotion map: %w", err)
			}
			emo = m
		} else {
			emo = emotion.DefaultMap()
		}
	}

	var overrides voice.Overrides
	if req.VoiceOverridesPath != "" {
		o, err := voice.LoadOverrides(req.VoiceOverridesPath)
		if err != nil {
			return nil, nil, "", fmt.Errorf("voice overrides: %w", err)
		}
		overrides = o
	}

	var prons *pron.Set
	if req.Pronunciations {
		if p := pronPathFor(req); p != "" {
			s, err := pron.LoadOrDefault(p)
			if err != nil {
				return nil, nil, "", fmt.Errorf("pronunciations: %w", err)
			}
			prons = s
		}
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
		Cleanup:        profile,
		Overrides:      overrides,
		Pronunciations: prons,
		Voices:         cfg,
		Layout:         layout,
		Emotion:        emo,
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
