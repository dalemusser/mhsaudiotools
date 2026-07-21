package job

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dalemusser/mhsaudiotools/engine/output"
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
		".aigenaudio-manifest.json",
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
