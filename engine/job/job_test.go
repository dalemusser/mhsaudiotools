package job

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dalemusser/mhsaudiotools/engine/emotion"
	"github.com/dalemusser/mhsaudiotools/engine/output"
	"github.com/dalemusser/mhsaudiotools/engine/pron"
	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/synth"
	"github.com/dalemusser/mhsaudiotools/engine/text"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// fakeClient records requests, can fail selected voices, and tracks peak concurrency.
type fakeClient struct {
	mu        sync.Mutex
	requests  []synth.Request
	failVoice string
	delay     time.Duration

	tier     string // reported by Subscription; "" means the call fails
	subCalls int64

	cur, peak int64
}

func (f *fakeClient) Subscription(ctx context.Context) (*synth.Subscription, error) {
	atomic.AddInt64(&f.subCalls, 1)
	if f.tier == "" {
		return nil, os.ErrNotExist
	}
	return &synth.Subscription{Tier: f.tier}, nil
}

func (f *fakeClient) Synthesize(ctx context.Context, req synth.Request) (*synth.Result, error) {
	n := atomic.AddInt64(&f.cur, 1)
	for {
		p := atomic.LoadInt64(&f.peak)
		if n <= p || atomic.CompareAndSwapInt64(&f.peak, p, n) {
			break
		}
	}
	defer atomic.AddInt64(&f.cur, -1)

	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()

	if f.failVoice != "" && req.VoiceID == f.failVoice {
		return nil, os.ErrPermission
	}
	return &synth.Result{
		Audio: []byte("AUDIO:" + req.VoiceID + ":" + req.Text),
		Words: []synth.WordTiming{{Word: "hi", Start: 0, End: 0.5}},
	}, nil
}

func (f *fakeClient) Voices(ctx context.Context) ([]synth.Voice, error) { return nil, nil }

func testConfig() *voice.Config {
	return &voice.Config{
		Assignments: []voice.Assignment{
			{Character: "Toppo", VoiceID: "v-toppo", VoiceName: "Amy"},
			{Character: "DANI", VoiceID: "v-dani", VoiceName: "Alex"},
		},
		PlayerSlots: []voice.Slot{
			{Index: 1, VoiceID: "v-p1", VoiceName: "Brayden"},
			{Index: 2, VoiceID: "v-p2", VoiceName: "Miranda"},
			{Index: 3, VoiceID: "v-p3", VoiceName: "Yomiee"},
		},
	}
}

func testLines() []source.LineItem {
	return []source.LineItem{
		{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."},
		{ID: "8_DANI_4", Speaker: "DANI", Text: "Classify this."},
		{ID: "8_Player_12", Speaker: "Player", Text: "Got it."},
	}
}

func newRunner(t *testing.T, c synth.Client, dir string, opts Options) *Runner {
	t.Helper()
	opts.OutputDir = dir
	return &Runner{
		Client:  c,
		Voices:  testConfig(),
		Layout:  output.DialogSystem{},
		Options: opts,
	}
}

func relFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(out)
	return out
}

// The core layout guarantee: NPCs flat at top level, players fanned out into
// Player<slot>/ with the same ID and that slot's voice.
func TestRunDialogSystemLayoutAndFanOut(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{Concurrency: 2})

	res, err := r.Run(context.Background(), testLines())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if res.Lines != 3 {
		t.Errorf("Lines = %d, want 3", res.Lines)
	}
	// 2 NPC lines + 1 player line x 3 slots = 5 files.
	if res.Targets != 5 || res.Written != 5 || res.Failed != 0 {
		t.Errorf("Targets=%d Written=%d Failed=%d, want 5/5/0", res.Targets, res.Written, res.Failed)
	}

	want := []string{
		".mhsaudio-manifest.json",
		"8_DANI_4.mp3",
		"8_Toppo_2.mp3",
		"Player1/8_Player_12.mp3",
		"Player2/8_Player_12.mp3",
		"Player3/8_Player_12.mp3",
	}
	got := relFiles(t, dir)
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file %d = %q, want %q", i, got[i], want[i])
		}
	}

	// Each player slot must render with its own pinned voice.
	for slot, voiceID := range map[int]string{1: "v-p1", 2: "v-p2", 3: "v-p3"} {
		path := filepath.Join(dir, "Player"+string(rune('0'+slot)), "8_Player_12.mp3")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if want := "AUDIO:" + voiceID + ":Got it."; string(data) != want {
			t.Errorf("Player%d content = %q, want %q", slot, data, want)
		}
	}
}

func TestRunIdempotentSkipAndForce(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})

	if _, err := r.Run(context.Background(), testLines()); err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Second run: everything is up to date, so nothing is synthesized.
	fc2 := &fakeClient{}
	r2 := newRunner(t, fc2, dir, Options{})
	res, err := r2.Run(context.Background(), testLines())
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if res.Written != 0 || res.SkippedFiles != 5 {
		t.Errorf("Written=%d SkippedFiles=%d, want 0/5", res.Written, res.SkippedFiles)
	}
	if len(fc2.requests) != 0 {
		t.Errorf("made %d API calls on an up-to-date run, want 0", len(fc2.requests))
	}

	// Changed text regenerates only that line.
	lines := testLines()
	lines[0].Text = "Hello again cadet."
	fc3 := &fakeClient{}
	r3 := newRunner(t, fc3, dir, Options{})
	res, err = r3.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if res.Written != 1 || res.SkippedFiles != 4 {
		t.Errorf("Written=%d SkippedFiles=%d, want 1/4 after a text change", res.Written, res.SkippedFiles)
	}

	// Force regenerates everything.
	fc4 := &fakeClient{}
	r4 := newRunner(t, fc4, dir, Options{Force: true})
	res, err = r4.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("force Run: %v", err)
	}
	if res.Written != 5 || res.SkippedFiles != 0 {
		t.Errorf("Written=%d SkippedFiles=%d, want 5/0 with Force", res.Written, res.SkippedFiles)
	}
}

func TestRunCleanupAppliedAndEmptyLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	r.Cleanup = &text.Profile{Rules: []text.Rule{
		{Kind: text.RuleLiteral, Op: text.OpRemove, From: "[em1]"},
		{Kind: text.RuleLiteral, Op: text.OpReplace, From: "WAT247", To: "Watt 2 4 7"},
		{Kind: text.RuleRegex, Op: text.OpRemove, From: `\{\{PLACEHOLDER[^}]*\}\}`},
	}}

	lines := []source.LineItem{
		{ID: "a", Speaker: "Toppo", Text: "[em1]Welcome to WAT247."},
		{ID: "b", Speaker: "Toppo", Text: "{{PLACEHOLDER - OPEN MAP}}"}, // cleans to nothing
	}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SkippedLines != 1 {
		t.Errorf("SkippedLines = %d, want 1", res.SkippedLines)
	}
	if res.Written != 1 {
		t.Errorf("Written = %d, want 1", res.Written)
	}
	if len(fc.requests) != 1 || fc.requests[0].Text != "Welcome to Watt 2 4 7." {
		t.Errorf("synthesized text = %+v, want cleaned 'Welcome to Watt 2 4 7.'", fc.requests)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.mp3")); !os.IsNotExist(err) {
		t.Error("expected no audio for a line that cleans to nothing")
	}
}

func TestRunConcurrencyIsCapped(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{delay: 20 * time.Millisecond}
	r := newRunner(t, fc, dir, Options{Concurrency: 2})

	var lines []source.LineItem
	for i := 0; i < 12; i++ {
		lines = append(lines, source.LineItem{
			ID: "line" + string(rune('a'+i)), Speaker: "Toppo", Text: "text",
		})
	}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak := atomic.LoadInt64(&fc.peak); peak > 2 {
		t.Errorf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestRunUnknownSpeakerIsIsolated(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})

	lines := []source.LineItem{
		{ID: "good", Speaker: "Toppo", Text: "fine"},
		{ID: "bad", Speaker: "Nobody", Text: "no voice for me"},
	}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Failed != 1 || len(res.Errors) != 1 {
		t.Fatalf("Failed=%d Errors=%v, want 1 failure", res.Failed, res.Errors)
	}
	if res.Errors[0].LineID != "bad" {
		t.Errorf("error line = %q, want %q", res.Errors[0].LineID, "bad")
	}
	// The good line still got generated.
	if res.Written != 1 {
		t.Errorf("Written = %d, want 1 (a bad line must not stop the batch)", res.Written)
	}
}

func TestRunSynthFailureRecorded(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{failVoice: "v-dani"}
	r := newRunner(t, fc, dir, Options{})

	res, err := r.Run(context.Background(), testLines())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Failed != 1 || res.Written != 4 {
		t.Errorf("Failed=%d Written=%d, want 1/4", res.Failed, res.Written)
	}
	if len(res.Errors) != 1 || res.Errors[0].LineID != "8_DANI_4" {
		t.Errorf("errors = %v, want one for 8_DANI_4", res.Errors)
	}
}

func TestRunDefaultVoiceForSpeakerlessLines(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	r.Voices.Default = &voice.Assignment{VoiceID: "v-default", VoiceName: "Amy"}

	// simplescript lines carry no speaker.
	lines := []source.LineItem{{ID: "u1argl1", Text: "Good morning, cadets."}}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("Written = %d, want 1", res.Written)
	}
	if fc.requests[0].VoiceID != "v-default" {
		t.Errorf("voice = %q, want v-default", fc.requests[0].VoiceID)
	}
	if _, err := os.Stat(filepath.Join(dir, "u1argl1.mp3")); err != nil {
		t.Errorf("expected u1argl1.mp3: %v", err)
	}
}

func TestRunWritesWordTimings(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{WithTimestamps: true})

	if _, err := r.Run(context.Background(), []source.LineItem{
		{ID: "a", Speaker: "Toppo", Text: "hi"},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fc.requests[0].WithTimestamps {
		t.Error("expected WithTimestamps to reach the client")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.words.json")); err != nil {
		t.Errorf("expected a.words.json sidecar: %v", err)
	}
}

// Concurrency: 0 means "use the account's full cap", detected from its tier.
func TestConcurrencyAutoDetectsFromTier(t *testing.T) {
	cases := []struct {
		tier string
		want int
	}{
		{"growing_business", 15}, // the project account
		{"business", 15},
		{"scale", 15},
		{"pro", 10},
		{"creator", 5},
		{"free", 2},
		{"some_new_tier", synth.ConservativeConcurrency}, // unknown -> conservative
		{"", synth.ConservativeConcurrency},              // API unreachable -> conservative
	}
	for _, tc := range cases {
		fc := &fakeClient{tier: tc.tier}
		r := newRunner(t, fc, t.TempDir(), Options{}) // Concurrency 0 = auto
		if got := r.concurrency(context.Background()); got != tc.want {
			t.Errorf("tier %q -> concurrency %d, want %d", tc.tier, got, tc.want)
		}
	}
}

func TestExplicitConcurrencyWinsAndSkipsDetection(t *testing.T) {
	fc := &fakeClient{tier: "growing_business"}
	r := newRunner(t, fc, t.TempDir(), Options{Concurrency: 2})
	if got := r.concurrency(context.Background()); got != 2 {
		t.Errorf("concurrency = %d, want the explicit 2", got)
	}
	if n := atomic.LoadInt64(&fc.subCalls); n != 0 {
		t.Errorf("Subscription called %d times, want 0 when concurrency is explicit", n)
	}
}

// Auto-detection must never fail a run just because the account lookup failed.
func TestRunSucceedsWhenSubscriptionLookupFails(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{} // tier "" -> Subscription errors
	r := newRunner(t, fc, dir, Options{})

	res, err := r.Run(context.Background(), testLines())
	if err != nil {
		t.Fatalf("Run must not fail when tier detection fails: %v", err)
	}
	if res.Written != 5 {
		t.Errorf("Written = %d, want 5", res.Written)
	}
}

// The heart of version-awareness: emotion tags are applied to v3 voices only,
// and each voice renders with its own model — including across a player line's
// mixed-model slots.
func TestRunVersionAwareEmotion(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	cfg := &voice.Config{
		Assignments: []voice.Assignment{
			{Character: "Toppo", VoiceID: "v-toppo", VoiceName: "Amy", Model: synth.ModelV3},
			{Character: "Aryn", VoiceID: "v-aryn", VoiceName: "Haseeb", Model: synth.ModelV2},
		},
		PlayerSlots: []voice.Slot{
			{Index: 1, VoiceID: "v-p1", VoiceName: "Brayden", Model: synth.ModelV2},
			{Index: 2, VoiceID: "v-p2", VoiceName: "Yomiee", Model: synth.ModelV3},
		},
	}
	r := &Runner{
		Client:  fc,
		Voices:  cfg,
		Layout:  output.DialogSystem{},
		Emotion: emotion.DefaultMap(),
		Options: Options{OutputDir: dir},
	}
	lines := []source.LineItem{
		{ID: "t1", Speaker: "Toppo", Text: "Hello (sighs) cadet."},   // v3 -> tag
		{ID: "a1", Speaker: "Aryn", Text: "Careful (angry) now."},    // v2 -> no tag
		{ID: "p1", Speaker: "Player", Text: "Understood (excited)!"}, // fan out: v2 slot + v3 slot
	}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Written != 4 { // 2 NPC + 1 player x 2 slots
		t.Fatalf("Written = %d, want 4", res.Written)
	}

	byVoice := map[string]synth.Request{}
	for _, req := range fc.requests {
		byVoice[req.VoiceID] = req
	}

	// Toppo is v3: tag applied, model v3.
	if r := byVoice["v-toppo"]; r.ModelID != synth.ModelV3 || !strings.HasPrefix(r.Text, "[sighs]") {
		t.Errorf("Toppo(v3) = model %q text %q; want v3 + [sighs] prefix", r.ModelID, r.Text)
	}
	// Aryn is v2: same source direction, but NO tag, model v2.
	if r := byVoice["v-aryn"]; r.ModelID != synth.ModelV2 || strings.Contains(r.Text, "[") {
		t.Errorf("Aryn(v2) = model %q text %q; want v2 + no tag", r.ModelID, r.Text)
	}
	// Player slot 1 (v2): no tag. Slot 2 (v3): tag. Same line, different delivery.
	if r := byVoice["v-p1"]; r.ModelID != synth.ModelV2 || strings.Contains(r.Text, "[") {
		t.Errorf("Player1(v2) = model %q text %q; want v2 + no tag", r.ModelID, r.Text)
	}
	if r := byVoice["v-p2"]; r.ModelID != synth.ModelV3 || !strings.HasPrefix(r.Text, "[excited]") {
		t.Errorf("Player2(v3) = model %q text %q; want v3 + [excited] prefix", r.ModelID, r.Text)
	}
}

// Without an emotion map, directions are left in the text for cleanup to handle,
// and the default model (v2) is used — i.e. current behavior is unchanged.
func TestRunNoEmotionUsesDefaultModel(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	if _, err := r.Run(context.Background(), []source.LineItem{
		{ID: "x", Speaker: "Toppo", Text: "Plain line."},
	}); err != nil {
		t.Fatal(err)
	}
	if got := fc.requests[0].ModelID; got != synth.ModelV2 {
		t.Errorf("model = %q, want default v2", got)
	}
}

func TestRunCancellation(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{delay: 50 * time.Millisecond}
	r := newRunner(t, fc, dir, Options{Concurrency: 1})

	var lines []source.LineItem
	for i := 0; i < 50; i++ {
		lines = append(lines, source.LineItem{
			ID: "l" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Speaker: "Toppo", Text: "text",
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	res, err := r.Run(ctx, lines)
	if err == nil {
		t.Fatal("expected a context error on cancellation")
	}
	if res == nil {
		t.Fatal("expected a partial result alongside the error")
	}
	if res.Written >= len(lines) {
		t.Errorf("Written = %d, expected cancellation to stop work early", res.Written)
	}
}

// Changing anything that determines a file's content — voice, model, format,
// or the timings toggle — must regenerate it even when the text is identical.
func TestRunRegeneratesWhenVoiceModelFormatOrTimingsChange(t *testing.T) {
	lines := []source.LineItem{{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."}}

	cases := []struct {
		name   string
		mutate func(r *Runner)
	}{
		{"voice", func(r *Runner) { r.Voices.Assignments[0].VoiceID = "v-other" }},
		{"model", func(r *Runner) { r.Options.DefaultModel = synth.ModelV3 }},
		{"format", func(r *Runner) { r.Options.Format = synth.AudioFormat("mp3_44100_192") }},
		{"timings", func(r *Runner) { r.Options.WithTimestamps = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			r := newRunner(t, &fakeClient{}, dir, Options{})
			if _, err := r.Run(context.Background(), lines); err != nil {
				t.Fatal(err)
			}
			tc.mutate(r)
			res, err := r.Run(context.Background(), lines)
			if err != nil {
				t.Fatal(err)
			}
			if res.Written != 1 {
				t.Fatalf("after %s change: written = %d, want 1 (stale audio kept)", tc.name, res.Written)
			}
		})
	}
}

// v1 manifests recorded only the text hash; those entries must keep matching
// so an upgrade doesn't silently re-pay for existing output folders.
func TestLegacyManifestEntriesStayUpToDate(t *testing.T) {
	dir := t.TempDir()
	lines := []source.LineItem{{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."}}
	r := newRunner(t, &fakeClient{}, dir, Options{})
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}

	legacy := `{"files": {"8_Toppo_2.mp3": "` + hashText("Hello cadet.") + `"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 0 || res.SkippedFiles != 1 {
		t.Fatalf("legacy entry regenerated: written=%d skipped=%d, want 0/1", res.Written, res.SkippedFiles)
	}
}

// A corrupt manifest must fail loudly, not silently regenerate the whole batch.
func TestCorruptManifestFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newRunner(t, &fakeClient{}, dir, Options{})
	if _, err := r.Run(context.Background(), testLines()); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("want corrupt-manifest error, got %v", err)
	}
	if _, err := r.Plan(testLines()); err == nil {
		t.Fatal("Plan accepted a corrupt manifest")
	}
}

// IDs with separators or dot-dot must be rejected at plan time and must never
// produce a file outside the output folder.
func TestHostileLineIDsAreRejected(t *testing.T) {
	dir := t.TempDir()
	lines := []source.LineItem{
		{ID: "../../escape", Speaker: "Toppo", Text: "hi"},
		{ID: `..\..\escape`, Speaker: "Toppo", Text: "hi"},
		{ID: "..", Speaker: "Toppo", Text: "hi"},
		{ID: "nested/inside", Speaker: "Toppo", Text: "hi"},
		{ID: "8_Toppo_2", Speaker: "Toppo", Text: "hi"}, // control: still generated
	}
	r := newRunner(t, &fakeClient{}, dir, Options{})
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 4 || res.Written != 1 {
		t.Fatalf("failed=%d written=%d, want 4 rejected / 1 written", res.Failed, res.Written)
	}
	for _, p := range []string{
		filepath.Join(dir, "..", "escape.mp3"),
		filepath.Join(dir, "..", "..", "escape.mp3"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("file escaped the output folder: %s", p)
		}
	}
}

// The manifest must land on disk periodically during a run, not only at the
// end, so a crash/kill loses at most a handful of files' records.
func TestManifestFlushedPeriodically(t *testing.T) {
	dir := t.TempDir()
	var lines []source.LineItem
	for i := 0; i < manifestFlushEvery+5; i++ {
		lines = append(lines, source.LineItem{
			ID: fmt.Sprintf("8_Toppo_%d", i), Speaker: "Toppo", Text: fmt.Sprintf("Line %d.", i),
		})
	}
	r := newRunner(t, &fakeClient{}, dir, Options{Concurrency: 4})
	var sawFlush atomic.Bool
	r.OnProgress = func(p Progress) {
		// The flush happens in the same critical section as this callback, so
		// by Done == manifestFlushEvery the file must exist.
		if p.Done == manifestFlushEvery && p.Failed == 0 {
			if _, err := os.Stat(filepath.Join(dir, manifestName)); err == nil {
				sawFlush.Store(true)
			}
		}
	}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	if !sawFlush.Load() {
		t.Fatal("manifest was not flushed during the run")
	}
}

// Progress must be monotonic and internally consistent under concurrency.
func TestProgressIsMonotonic(t *testing.T) {
	dir := t.TempDir()
	var lines []source.LineItem
	for i := 0; i < 40; i++ {
		lines = append(lines, source.LineItem{
			ID: fmt.Sprintf("8_Toppo_%d", i), Speaker: "Toppo", Text: fmt.Sprintf("Line %d.", i),
		})
	}
	r := newRunner(t, &fakeClient{}, dir, Options{Concurrency: 8})
	var mu sync.Mutex
	last := -1
	r.OnProgress = func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		if p.Done < last {
			t.Errorf("progress went backwards: %d after %d", p.Done, last)
		}
		last = p.Done
	}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
}

// A legacy (v1) manifest entry with -timestamps must be grandfathered when the
// sidecar already exists on disk — the v1 tool wrote sidecars too, and a whole
// batch must not regenerate just because the manifest predates the Words field.
// Without the sidecar it must regenerate (that's what produces it).
func TestLegacyManifestWithTimestampsUsesSidecarOnDisk(t *testing.T) {
	lines := []source.LineItem{{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."}}

	setup := func(t *testing.T) (string, *Runner) {
		dir := t.TempDir()
		r := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true})
		if _, err := r.Run(context.Background(), lines); err != nil {
			t.Fatal(err)
		}
		legacy := `{"files": {"8_Toppo_2.mp3": "` + hashText("Hello cadet.") + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(legacy), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir, r
	}

	t.Run("sidecar present: up to date", func(t *testing.T) {
		_, r := setup(t)
		res, err := r.Run(context.Background(), lines)
		if err != nil {
			t.Fatal(err)
		}
		if res.Written != 0 || res.SkippedFiles != 1 {
			t.Fatalf("written=%d skipped=%d, want 0/1 (sidecar on disk must grandfather)", res.Written, res.SkippedFiles)
		}
	})

	t.Run("sidecar deleted: regenerates", func(t *testing.T) {
		dir, r := setup(t)
		if err := os.Remove(filepath.Join(dir, "8_Toppo_2.words.json")); err != nil {
			t.Fatal(err)
		}
		res, err := r.Run(context.Background(), lines)
		if err != nil {
			t.Fatal(err)
		}
		if res.Written != 1 {
			t.Fatalf("written=%d, want 1 (missing sidecar must regenerate)", res.Written)
		}
	})
}

// A hostile speaker must not cost a paid API call on every run: the Babylon
// layout splices the speaker into the path, and the reject has to happen at
// plan time, before synthesis.
func TestHostileSpeakerRejectedBeforeSynthesis(t *testing.T) {
	dir := t.TempDir()
	client := &fakeClient{}
	r := newRunner(t, client, dir, Options{})
	r.Layout = output.BabylonManifest{}
	r.Voices.Assignments = append(r.Voices.Assignments, voice.Assignment{
		Character: "..", VoiceID: "v-evil", VoiceName: "Evil",
	})
	lines := []source.LineItem{
		{ID: "L1", Speaker: "..", Text: "escape attempt"},
		{ID: "L2", Speaker: "Toppo", Text: "fine"},
	}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed != 1 || res.Written != 1 {
		t.Fatalf("failed=%d written=%d, want 1/1", res.Failed, res.Written)
	}
	for _, req := range client.requests {
		if req.VoiceID == "v-evil" {
			t.Fatal("paid a synthesis call for a target that can never be written")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "L1.mp3")); !os.IsNotExist(err) {
		t.Fatal("file escaped the output folder")
	}
}

// Windows filename hazards are rejected at plan time on every platform, so a
// batch generated on macOS still lands intact on a Windows checkout.
func TestWindowsHostileIDsRejected(t *testing.T) {
	for _, id := range []string{"scene:1", "NUL", "con.mp3", "trailing.", "trailing "} {
		if err := checkID(id); err == nil {
			t.Errorf("checkID(%q) = nil, want error", id)
		}
	}
	for _, id := range []string{"8_Toppo_2", "console_log", "NULL_check", "a.b.c"} {
		if err := checkID(id); err != nil {
			t.Errorf("checkID(%q) = %v, want nil", id, err)
		}
	}
}

// The Babylon layout must emit the ceremony player's manifest after a run,
// covering every non-failed file (including up-to-date ones on a re-run), with
// [startSec, word] caption pairs read from the sidecars.
func TestBabylonLayoutEmitsCeremonyManifest(t *testing.T) {
	dir := t.TempDir()
	r := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true})
	r.Layout = output.BabylonManifest{}
	lines := []source.LineItem{
		{ID: "c1", Speaker: "Toppo", Text: "Welcome."},
		{ID: "c2", Speaker: "DANI", Text: "Standing by."},
	}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.LayoutManifest == "" {
		t.Fatal("LayoutManifest not set")
	}

	read := func() map[string]any {
		data, err := os.ReadFile(filepath.Join(dir, "ceremony_audio.json"))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	check := func(m map[string]any) {
		items := m["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("items = %d, want 2", len(items))
		}
		first := items[0].(map[string]any)
		if first["speaker"] != "Toppo" || first["audio"] != "assets/audio/Toppo/c1.mp3" {
			t.Fatalf("first item wrong: %v", first)
		}
		words := first["words"].([]any)
		if len(words) == 0 {
			t.Fatal("words missing — sidecar not folded in")
		}
		pair := words[0].([]any)
		if _, isNum := pair[0].(float64); !isNum || pair[1] != "hi" {
			t.Fatalf("word pair shape wrong: %v", pair)
		}
		if first["durationSec"].(float64) <= 0 {
			t.Fatal("durationSec missing")
		}
		if first["textHash"] == "" || first["id"] != "c1" {
			t.Fatalf("id/textHash wrong: %v", first)
		}
	}
	check(read())

	// An incremental re-run (everything up to date) must still describe the
	// whole set, not just what it wrote.
	res2, err := newRunnerWithLayout(t, dir).Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Written != 0 {
		t.Fatalf("second run wrote %d, want 0", res2.Written)
	}
	check(read())
}

func newRunnerWithLayout(t *testing.T, dir string) *Runner {
	t.Helper()
	r := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true})
	r.Layout = output.BabylonManifest{}
	return r
}

func fp(v float64) *float64 { return &v }

// Per-voice settings reach the API request; a per-line override wins for its
// audible knobs; and changing either regenerates exactly the affected files.
func TestVoiceSettingsAndLineOverrides(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	r.Voices.Assignments[0].Settings = &voice.Settings{Stability: fp(0.4), Speed: fp(1.1)}
	r.Overrides = voice.Overrides{
		"8_Toppo_2": {Stability: fp(0.2)}, // override wins over the voice's 0.4
	}
	lines := []source.LineItem{
		{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."},
		{ID: "8_DANI_4", Speaker: "DANI", Text: "Classify this."}, // no settings at all
	}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}

	byVoice := map[string]*synth.VoiceSettings{}
	for _, req := range fc.requests {
		byVoice[req.VoiceID] = req.VoiceSettings
	}
	s := byVoice["v-toppo"]
	if s == nil || s.Stability == nil || *s.Stability != 0.2 || s.Speed == nil || *s.Speed != 1.1 {
		t.Fatalf("toppo settings = %+v, want stability 0.2 (override) + speed 1.1 (voice)", s)
	}
	if byVoice["v-dani"] != nil {
		t.Fatalf("dani got settings %+v, want none (voice defaults)", byVoice["v-dani"])
	}

	// Identical settings: everything up to date.
	r2 := newRunner(t, &fakeClient{}, dir, Options{})
	r2.Voices.Assignments[0].Settings = &voice.Settings{Stability: fp(0.4), Speed: fp(1.1)}
	r2.Overrides = voice.Overrides{"8_Toppo_2": {Stability: fp(0.2)}}
	res, err := r2.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 0 {
		t.Fatalf("unchanged settings regenerated %d files", res.Written)
	}

	// Tweaking the override regenerates only that line's file.
	r3 := newRunner(t, &fakeClient{}, dir, Options{})
	r3.Voices.Assignments[0].Settings = &voice.Settings{Stability: fp(0.4), Speed: fp(1.1)}
	r3.Overrides = voice.Overrides{"8_Toppo_2": {Stability: fp(0.3)}}
	res, err = r3.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || res.SkippedFiles != 1 {
		t.Fatalf("written=%d skipped=%d, want 1/1 after override change", res.Written, res.SkippedFiles)
	}
}

// A player line's override applies to every slot's rendering of that line.
func TestLineOverrideAppliesToAllPlayerSlots(t *testing.T) {
	dir := t.TempDir()
	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	r.Overrides = voice.Overrides{"8_Player_12": {Speed: fp(0.9)}}
	lines := []source.LineItem{{ID: "8_Player_12", Speaker: "Player", Text: "Got it."}}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	if len(fc.requests) != 3 {
		t.Fatalf("requests = %d, want 3 slots", len(fc.requests))
	}
	for _, req := range fc.requests {
		if req.VoiceSettings == nil || req.VoiceSettings.Speed == nil || *req.VoiceSettings.Speed != 0.9 {
			t.Fatalf("slot %s settings = %+v, want speed 0.9", req.VoiceID, req.VoiceSettings)
		}
	}
}

// Pronunciation dictionary: the locator rides on every request while the text
// keeps the writers' spelling; editing a rule regenerates only affected lines;
// an unpublished set refuses to run.
func TestPronunciationDictionaryFlow(t *testing.T) {
	dir := t.TempDir()
	set := pron.DefaultSet()
	set.DictionaryID, set.VersionID, set.PublishedHash = "dict-1", "ver-1", set.RulesHash()

	fc := &fakeClient{}
	r := newRunner(t, fc, dir, Options{})
	r.Cleanup = text.MHSProfile()
	r.Pronunciations = set
	lines := []source.LineItem{
		{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Welcome to WAT247."},
		{ID: "8_DANI_4", Speaker: "DANI", Text: "Nothing special here."},
	}
	if _, err := r.Run(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	for _, req := range fc.requests {
		if len(req.DictionaryLocators) != 1 || req.DictionaryLocators[0].ID != "dict-1" {
			t.Fatalf("request missing locator: %+v", req.DictionaryLocators)
		}
		if strings.Contains(req.Text, "Watt 2 4 7") {
			t.Fatalf("text was rewritten client-side: %q", req.Text)
		}
	}
	var sawOriginal bool
	for _, req := range fc.requests {
		if strings.Contains(req.Text, "WAT247") {
			sawOriginal = true
		}
	}
	if !sawOriginal {
		t.Fatal("original spelling never reached the API")
	}

	// Editing the matching rule regenerates only the affected line.
	set2 := pron.DefaultSet()
	set2.Rules["WAT247"] = "Watt two four seven"
	set2.DictionaryID, set2.VersionID, set2.PublishedHash = "dict-1", "ver-2", set2.RulesHash()
	r2 := newRunner(t, &fakeClient{}, dir, Options{})
	r2.Cleanup = text.MHSProfile()
	r2.Pronunciations = set2
	res, err := r2.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 1 || res.SkippedFiles != 1 {
		t.Fatalf("written=%d skipped=%d, want 1/1 (only the WAT247 line)", res.Written, res.SkippedFiles)
	}

	// Unpublished rules must refuse to run rather than silently mispronounce.
	r3 := newRunner(t, &fakeClient{}, dir, Options{})
	r3.Pronunciations = pron.DefaultSet()
	if _, err := r3.Run(context.Background(), lines); err == nil || !strings.Contains(err.Error(), "publish") {
		t.Fatalf("want unpublished error, got %v", err)
	}
}

// Orphan detection + prune: files from removed lines are found, deleted with
// their sidecars and empty folders, and unknown files are never touched.
func TestPruneOrphans(t *testing.T) {
	dir := t.TempDir()
	r := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true})
	if _, err := r.Run(context.Background(), testLines()); err != nil {
		t.Fatal(err)
	}

	// A file the tool didn't create must be invisible to pruning.
	foreign := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(foreign, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop the player line from the source: its three slot files are orphans.
	kept := testLines()[:2]
	plan, err := r.Plan(kept)
	if err != nil {
		t.Fatal(err)
	}
	// Orphans list audio relpaths only; sidecars are deleted alongside.
	if len(plan.Orphans) != 3 {
		t.Fatalf("orphans = %v, want the 3 player files", plan.Orphans)
	}

	deleted, err := r.PruneOrphans(kept)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 3 {
		t.Fatalf("deleted = %v, want 3 player files", deleted)
	}
	for _, rel := range deleted {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("%s still on disk", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Player1")); !os.IsNotExist(err) {
		t.Fatal("empty Player1/ folder not removed")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("pruning touched a file the tool didn't create")
	}

	// Second prune: nothing left to do, and the survivors are intact.
	deleted, err = r.PruneOrphans(kept)
	if err != nil || len(deleted) != 0 {
		t.Fatalf("second prune = %v, %v; want empty", deleted, err)
	}
	res, err := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true}).Run(context.Background(), kept)
	if err != nil || res.Written != 0 || res.SkippedFiles != 2 {
		t.Fatalf("survivors disturbed: %+v, %v", res, err)
	}
}

// Pruning must refuse to act on a plan with problems — a broken voices config
// would otherwise make perfectly good files look orphaned.
func TestPruneRefusesOnPlanProblems(t *testing.T) {
	dir := t.TempDir()
	r := newRunner(t, &fakeClient{}, dir, Options{})
	if _, err := r.Run(context.Background(), testLines()); err != nil {
		t.Fatal(err)
	}
	r2 := newRunner(t, &fakeClient{}, dir, Options{})
	r2.Voices = &voice.Config{ // no assignments: every line fails planning
		PlayerSlots: testConfig().PlayerSlots,
	}
	if _, err := r2.PruneOrphans(testLines()); err == nil {
		t.Fatal("prune accepted a plan full of problems")
	}
	if p, _ := r.Plan(testLines()); len(p.Orphans) != 0 {
		t.Fatalf("healthy plan reported orphans: %v", p.Orphans)
	}
}

// WrittenFiles must list exactly what THIS run wrote — audio plus sidecars —
// and CopyFiles must reproduce them under the destination.
func TestWrittenFilesAndCopyFiles(t *testing.T) {
	dir := t.TempDir()
	r := newRunner(t, &fakeClient{}, dir, Options{WithTimestamps: true})
	lines := []source.LineItem{{ID: "8_Toppo_2", Speaker: "Toppo", Text: "Hello cadet."}}
	res, err := r.Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"8_Toppo_2.mp3", "8_Toppo_2.words.json"}
	if len(res.WrittenFiles) != 2 || res.WrittenFiles[0] != want[0] || res.WrittenFiles[1] != want[1] {
		t.Fatalf("WrittenFiles = %v, want %v", res.WrittenFiles, want)
	}

	delta := t.TempDir()
	if err := CopyFiles(dir, delta, res.WrittenFiles); err != nil {
		t.Fatal(err)
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(delta, rel)); err != nil {
			t.Fatalf("delta missing %s", rel)
		}
	}

	// An incremental re-run with one changed line writes only that file.
	lines[0].Text = "Hello again."
	res, err = newRunner(t, &fakeClient{}, dir, Options{}).Run(context.Background(), lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.WrittenFiles) != 1 || res.WrittenFiles[0] != "8_Toppo_2.mp3" {
		t.Fatalf("incremental WrittenFiles = %v", res.WrittenFiles)
	}
}
