package emotion

import (
	"reflect"
	"testing"
)

func TestExtract(t *testing.T) {
	clean, dirs := Extract("Hello (sighs) cadet, we go (angry) north.")
	if clean != "Hello cadet, we go north." {
		t.Errorf("clean = %q", clean)
	}
	if !reflect.DeepEqual(dirs, []string{"sighs", "angry"}) {
		t.Errorf("directions = %v", dirs)
	}

	// No parentheticals: text unchanged, no directions.
	clean, dirs = Extract("Just a plain line.")
	if clean != "Just a plain line." || len(dirs) != 0 {
		t.Errorf("plain line: clean=%q dirs=%v", clean, dirs)
	}

	// Empty parens are dropped, not returned as a direction.
	_, dirs = Extract("Well () then.")
	if len(dirs) != 0 {
		t.Errorf("empty parens should yield no directions, got %v", dirs)
	}
}

func TestTagsFor(t *testing.T) {
	m := DefaultMap()

	if got := m.TagsFor([]string{"sighs", "angry"}); !reflect.DeepEqual(got, []string{"[sighs]", "[angry]"}) {
		t.Errorf("tags = %v", got)
	}
	// Case-insensitive, and synonyms collapse (sighs & sigh -> one [sighs]).
	if got := m.TagsFor([]string{"Sighs", "sigh"}); !reflect.DeepEqual(got, []string{"[sighs]"}) {
		t.Errorf("dedup/case = %v", got)
	}
	// Ignored directions produce no tag.
	if got := m.TagsFor([]string{"calling out from the island"}); len(got) != 0 {
		t.Errorf("ignored should be empty, got %v", got)
	}
	// Unknown directions are left out (no guessing).
	if got := m.TagsFor([]string{"ponders quietly"}); len(got) != 0 {
		t.Errorf("unknown should be empty, got %v", got)
	}
}

func TestApply(t *testing.T) {
	m := DefaultMap()
	if got := m.Apply("Let us begin.", []string{"excited"}); got != "[excited] Let us begin." {
		t.Errorf("apply = %q", got)
	}
	// No mapped tags -> text unchanged.
	if got := m.Apply("Let us begin.", []string{"unmapped"}); got != "Let us begin." {
		t.Errorf("apply (no tags) = %q", got)
	}
	// Nil map is safe.
	var nm *Map
	if got := nm.Apply("x", []string{"sighs"}); got != "x" {
		t.Errorf("nil map apply = %q", got)
	}
}

// Nested directions must peel cleanly — no "(angry " fragment may survive into
// the spoken text — while unbalanced parens stay visible on purpose.
func TestExtractNestedAndUnbalancedParens(t *testing.T) {
	clean, dirs := Extract("Hello (angry (very)) cadet")
	if clean != "Hello cadet" {
		t.Errorf("clean = %q, want %q", clean, "Hello cadet")
	}
	if len(dirs) != 2 || dirs[0] != "very" || dirs[1] != "angry" {
		t.Errorf("directions = %v, want [very angry]", dirs)
	}

	clean, dirs = Extract("(sighs I forgot to close this")
	if clean != "(sighs I forgot to close this" || len(dirs) != 0 {
		t.Errorf("unbalanced parens must pass through, got %q %v", clean, dirs)
	}
}

// Peeling a nested parenthetical leaves doubled spaces in the outer direction;
// the map lookup must still match a single-spaced phrase.
func TestNestedDirectionWhitespaceStillMatchesMap(t *testing.T) {
	m := &Map{Tags: map[string]string{"angry shout": "[shouts]"}}
	_, dirs := Extract("(angry (very) shout) hi")
	got := m.Apply("hi", dirs)
	if got != "[shouts] hi" {
		t.Fatalf("got %q, want %q", got, "[shouts] hi")
	}
}
