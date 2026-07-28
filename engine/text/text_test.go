package text

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMHSProfileMatchesPriorBehavior(t *testing.T) {
	p := MHSProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("MHSProfile has an invalid rule: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"emphasis markup stripped",
			"[em1]Listen up[/em1] cadet.",
			"Listen up cadet.",
		},
		{
			"placeholder stage direction stripped",
			"Follow me. {{PLACEHOLDER - OPEN MAP MENU}} We go north.",
			"Follow me. We go north.",
		},
		{
			"malformed placeholder (single brace) still stripped",
			"Wait. {{PLACEHOLDER - PLAYER CLOSES MENU} Now go.",
			"Wait. Now go.",
		},
		{
			"double-bracket placeholder stripped",
			"Ready? [[PLACEHOLDER - Argument]] Begin.",
			"Ready? Begin.",
		},
		{
			"color markup stripped",
			"The <color=#35F>water</color> is clean.",
			"The water is clean.",
		},
		{
			// Pronunciation moved to the server-side dictionary (engine/pron):
			// cleanup must now leave these words alone, so captions and word
			// timings align to the displayed text.
			"pronunciation words left untouched",
			"Welcome to WAT247, this is Mission HydroSci. DANI out.",
			"Welcome to WAT247, this is Mission HydroSci. DANI out.",
		},
		{
			"smart apostrophe normalized",
			"It’s fine.",
			"It's fine.",
		},
		{
			"dashes become spaces",
			"east-west — north–south",
			"east west north south",
		},
		{
			"quotes removed",
			`She said "hello" and “goodbye”.`,
			"She said hello and goodbye.",
		},
		{
			"literal backslash-n from the export removed",
			`Line one.\nLine two.`,
			"Line one.Line two.",
		},
		{
			// The regression this profile fixes: the Python deleted ellipses, fusing words.
			"ellipsis between words no longer fuses them",
			"Starting with…some systems…Running now.",
			"Starting with some systems Running now.",
		},
		{
			// ...while spaced ellipses still collapse to exactly one space, as before.
			"spaced ellipsis behaves as it always did",
			"using knowledge… facts and logic",
			"using knowledge facts and logic",
		},
		{
			"stage direction in parens removed",
			"(brightly) Good morning!",
			"Good morning!",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Apply(tc.in)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Errorf("Apply(%q)\n got %q\nwant %q", tc.in, strings.TrimSpace(got), tc.want)
			}
		})
	}
}

// A line that is nothing but a stage direction must clean to nothing, so the job
// skips it instead of paying to synthesize silence.
func TestMHSProfileCleansDirectionOnlyLineToNothing(t *testing.T) {
	got, err := MHSProfile().Apply("{{PLACEHOLDER - TRANSITION TO BASE CAMP}}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRuleOrderIsPreserved(t *testing.T) {
	// en-dash -> space must run before hyphen -> space; reversing them would
	// still work here, but the ordering contract is what matters.
	p := &Profile{Rules: []Rule{
		{Kind: RuleLiteral, Op: OpReplace, From: "ab", To: "X"},
		{Kind: RuleLiteral, Op: OpReplace, From: "X", To: "Y"},
	}}
	got, err := p.Apply("ab")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Y" {
		t.Errorf("got %q, want %q — rules must apply in order", got, "Y")
	}
}

func TestProfileSaveLoadRoundTripUsesReadableNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles", "mhs.json")
	if err := MHSProfile().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadProfile(path)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if got.Name != "mhs-dialogue" || len(got.Rules) != len(MHSProfile().Rules) {
		t.Fatalf("round trip lost data: name=%q rules=%d", got.Name, len(got.Rules))
	}
	// Behavior must survive the round trip (markup stripped; pronunciation
	// words untouched — that's the dictionary's job now).
	out, err := got.Apply("[em1]Welcome to WAT247.[/em1]")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "Welcome to WAT247." {
		t.Errorf("after round trip: %q", out)
	}
}

func TestLoadProfileRejectsBadRegex(t *testing.T) {
	dir := t.TempDir()
	bad := &Profile{Name: "bad", Rules: []Rule{{Kind: RuleRegex, Op: OpRemove, From: "([unclosed"}}}
	path := filepath.Join(dir, "bad.json")
	if err := bad.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path); err == nil {
		t.Error("expected LoadProfile to reject an invalid regex up front")
	}
}

// One profile may be applied from several goroutines at once (app preview
// concurrent with a run); the regex compile caches must be race-free.
func TestProfileApplyConcurrent(t *testing.T) {
	p := &Profile{Name: "t", Rules: []Rule{
		{Kind: RuleRegex, Op: OpRemove, From: `\[em\d\]`},
		{Kind: RuleLiteral, Op: OpReplace, From: "WAT247", To: "Watt 2 4 7"},
	}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if _, err := p.Apply("[em1] WAT247 hello"); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// An empty "from" would make a replace insert its text between every character
// of every line — Validate must refuse it before it can reach a paid run.
func TestValidateRejectsEmptyFrom(t *testing.T) {
	p := &Profile{Name: "bad", Rules: []Rule{{Kind: RuleLiteral, Op: OpReplace, From: "", To: "x"}}}
	if err := p.Validate(); err == nil {
		t.Fatal("empty-from rule passed validation")
	}
}
