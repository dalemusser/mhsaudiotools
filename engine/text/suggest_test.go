package text

import "testing"

func findByDesc(sugs []Suggestion, substr string) *Suggestion {
	for i := range sugs {
		if containsFold(sugs[i].Description, substr) {
			return &sugs[i]
		}
	}
	return nil
}

func containsFold(s, sub string) bool {
	// tiny case-insensitive contains, avoids importing strings just for tests
	for i := 0; i+len(sub) <= len(s); i++ {
		if eqFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}
func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 32
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// The core behavior: scanning through the MHS profile surfaces only NEW markup,
// not what the profile already removes.
func TestSuggestSurfacesOnlyUnhandled(t *testing.T) {
	texts := []string{
		"[em1]Welcome[/em1] to WAT247.",        // [em1] IS handled by MHSProfile
		"{{PLACEHOLDER - OPEN MAP}} go north.", // handled by MHSProfile
		"Careful [shout] and [whisper] there.", // NOT handled -> should surface
		"Use the {{DRONE}} now.",               // {{DRONE}} not 'placeholder' -> surfaces
		"The <blink>light</blink> flashes.",    // angle tags -> surface
		"Nothing to see here.",                 // clean
	}

	got, err := Suggest(texts, MHSProfile())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	// [em1]/[/em1] and {{PLACEHOLDER…}} must NOT appear (already removed).
	if s := findByDesc(got, "square-bracket"); s != nil {
		for _, ex := range s.Examples {
			if ex == "[em1]" || ex == "[/em1]" {
				t.Errorf("re-suggested an already-handled token: %q", ex)
			}
		}
		// but it SHOULD have caught the new [shout]/[whisper]
		if s.Count < 2 {
			t.Errorf("expected [shout]/[whisper] surfaced, got count %d", s.Count)
		}
	} else {
		t.Error("expected a square-bracket suggestion for [shout]/[whisper]")
	}

	if s := findByDesc(got, "double-brace"); s != nil {
		for _, ex := range s.Examples {
			if ex == "{{PLACEHOLDER - OPEN MAP}}" {
				t.Errorf("re-suggested handled placeholder: %q", ex)
			}
		}
	}

	if findByDesc(got, "angle") == nil {
		t.Error("expected an angle-tag suggestion for <blink>/</blink>")
	}
}

// Doubled delimiters must not be double-counted by the single-delimiter detectors.
func TestSuggestNoDoubleReporting(t *testing.T) {
	got, err := Suggest([]string{"a {{FOO}} b [[BAR]] c"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	brace := findByDesc(got, "brace tags {")
	if brace != nil {
		t.Errorf("single-brace detector re-reported inside of {{ }}: %+v", brace)
	}
	bracket := findByDesc(got, "square-bracket")
	if bracket != nil {
		t.Errorf("single-bracket detector re-reported inside of [[ ]]: %+v", bracket)
	}
	if findByDesc(got, "double-brace") == nil || findByDesc(got, "double-bracket") == nil {
		t.Error("expected the double-delimiter families to be reported")
	}
}

func TestSuggestExamplesRankedByFrequency(t *testing.T) {
	texts := []string{"<a> <a> <a> <b>"}
	got, err := Suggest(texts, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := findByDesc(got, "angle")
	if s == nil {
		t.Fatal("expected angle suggestion")
	}
	if s.Count != 4 {
		t.Errorf("count = %d, want 4", s.Count)
	}
	if len(s.Examples) == 0 || s.Examples[0] != "<a>" {
		t.Errorf("most frequent example = %v, want <a> first", s.Examples)
	}
}

func TestSuggestionToRule(t *testing.T) {
	s := Suggestion{Description: "angle tags < … >", Pattern: `<[^<>]*>`}
	r := s.Rule()
	if r.Kind != RuleRegex || r.Op != OpRemove || r.From != `<[^<>]*>` {
		t.Errorf("Rule() = %+v", r)
	}
	// The produced rule must actually be valid and remove the token.
	p := &Profile{Rules: []Rule{r}}
	out, err := p.Apply("keep <b>drop</b> keep")
	if err != nil {
		t.Fatal(err)
	}
	if out != "keep drop keep" {
		t.Errorf("applying suggested rule gave %q", out)
	}
}
