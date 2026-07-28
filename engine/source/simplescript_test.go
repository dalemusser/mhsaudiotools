package source

import (
	"strings"
	"testing"
)

func TestSimpleScriptParse(t *testing.T) {
	data := strings.Join([]string{
		"u1argl1: Good morning, cadets! Today we learn about argumentation.",
		"", // blank -> skip
		"u1argl2:   Arguments answer a driving question.  ",                       // leading/trailing space trimmed
		"a good scientific argument has three parts: claim, evidence, reasoning.", // no single-token id -> still splits on first colon
		"u1argl1: A duplicate id.",                                                // duplicate -> u1argl1_2
		"noseparatorhere",                                                         // no colon -> skip
		"  : text with empty id",                                                  // empty id -> skip
		"u1argl3:   ",                                                             // empty text -> skip
	}, "\n")

	got, err := SimpleScript{}.Parse(strings.NewReader(data))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	want := []LineItem{
		{ID: "u1argl1", Text: "Good morning, cadets! Today we learn about argumentation."},
		{ID: "u1argl2", Text: "Arguments answer a driving question."},
		{ID: "a good scientific argument has three parts", Text: "claim, evidence, reasoning."},
		{ID: "u1argl1_2", Text: "A duplicate id."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].ID != w.ID || got[i].Text != w.Text {
			t.Errorf("item %d = {ID:%q Text:%q}, want {ID:%q Text:%q}",
				i, got[i].ID, got[i].Text, w.ID, w.Text)
		}
		if got[i].Speaker != "" {
			t.Errorf("item %d Speaker = %q, want empty", i, got[i].Speaker)
		}
	}
}

func TestSimpleScriptDetect(t *testing.T) {
	yes := "u1argl1: Good morning.\nu1argl2: Arguments answer a question.\nu2topol1: Today, topography."
	no := "This is a paragraph of prose. It has a colon: right here.\nBut it is clearly not an id/text script, just sentences."

	if !(SimpleScript{}).Detect([]byte(yes)) {
		t.Error("expected Detect to accept an id/text script")
	}
	if (SimpleScript{}).Detect([]byte(no)) {
		t.Error("expected Detect to reject prose")
	}
}

// A UTF-8 BOM (Excel/Notepad) must not become an invisible prefix on the first
// line's ID — the resulting filename would never match the game's lookup.
func TestSimpleScriptStripsBOM(t *testing.T) {
	items, err := SimpleScript{}.Parse(strings.NewReader("\ufeffu1argl1: Good morning.\nu1argl2: Hello.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].ID != "u1argl1" {
		t.Fatalf("first ID = %q, want %q (BOM leaked in)", items[0].ID, "u1argl1")
	}
	if !(SimpleScript{}).Detect([]byte("\ufeffu1argl1: Good morning.\nu1argl2: Hello.\n")) {
		t.Fatal("Detect failed on BOM'd sample")
	}
}

// Duplicate suffixing must never itself collide with a literal ID in the file.
func TestSimpleScriptDuplicateSuffixCannotCollide(t *testing.T) {
	items, err := SimpleScript{}.Parse(strings.NewReader("a: one\na: two\na_2: three\na: four\n"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.ID] {
			t.Fatalf("duplicate ID %q in output: %+v", it.ID, items)
		}
		seen[it.ID] = true
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4", len(items))
	}
}

// The speaker variant: "ID | Speaker: text" lines carry a speaker (Player fans
// out downstream), plain "ID: text" lines stay speaker-less, and both forms mix
// freely in one file.
func TestSimpleScriptSpeakerVariant(t *testing.T) {
	in := "u1 | Toppo: Welcome back.\n" +
		"u2: Narration with no speaker.\n" +
		"u3 | Mission Control: Copy: that.\n" +
		"u4 | Player: On my way.\n"
	items, err := SimpleScript{}.Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ id, speaker, text string }{
		{"u1", "Toppo", "Welcome back."},
		{"u2", "", "Narration with no speaker."},
		{"u3", "Mission Control", "Copy: that."},
		{"u4", "Player", "On my way."},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].ID != w.id || items[i].Speaker != w.speaker || items[i].Text != w.text {
			t.Errorf("item %d = {%q %q %q}, want {%q %q %q}",
				i, items[i].ID, items[i].Speaker, items[i].Text, w.id, w.speaker, w.text)
		}
	}
	if !items[3].IsPlayer() {
		t.Error("Player speaker must trigger fan-out")
	}
	if !(SimpleScript{}).Detect([]byte(in)) {
		t.Error("Detect failed on speaker-variant sample")
	}
}
