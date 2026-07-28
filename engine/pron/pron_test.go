package pron

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dalemusser/mhsaudiotools/engine/synth"
)

func TestRoundTripAndPublishState(t *testing.T) {
	s := DefaultSet()
	if !s.Active() || s.NeedsPublish() == false {
		t.Fatal("default set must be active and need publishing")
	}

	// Publishing records the locator + content hash.
	s.DictionaryID, s.VersionID, s.PublishedHash = "d1", "v1", s.RulesHash()
	if s.NeedsPublish() {
		t.Fatal("freshly published set must not need publishing")
	}

	// Editing a rule makes it stale again.
	s.Rules["Toppo"] = "Top-oh"
	if !s.NeedsPublish() {
		t.Fatal("edited rules must need republishing")
	}

	path := filepath.Join(t.TempDir(), "pronunciations.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rules["Toppo"] != "Top-oh" || got.DictionaryID != "d1" || got.PublishedHash == got.RulesHash() {
		t.Fatalf("round trip lost state: %+v", got)
	}
}

func TestLoadOrDefaultMissingFile(t *testing.T) {
	s, err := LoadOrDefault(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || !s.Active() {
		t.Fatalf("missing file must yield the default set, got %v / %v", s, err)
	}
}

func TestAffectedKeyIsPerLine(t *testing.T) {
	s := DefaultSet()
	if k := s.AffectedKey("Nothing special here."); k != "" {
		t.Fatalf("unaffected line got key %q", k)
	}
	k1 := s.AffectedKey("Welcome to WAT247.")
	if k1 == "" {
		t.Fatal("affected line got no key")
	}
	// Editing an UNRELATED rule must not change this line's key…
	s.Rules["Toppo"] = "Top-oh"
	if s.AffectedKey("Welcome to WAT247.") != k1 {
		t.Fatal("unrelated rule edit changed an affected line's key")
	}
	// …while editing the matching rule must.
	s.Rules["WAT247"] = "Watt two four seven"
	if s.AffectedKey("Welcome to WAT247.") == k1 {
		t.Fatal("matching rule edit did not change the key")
	}
	// A nil set is inert.
	var nilSet *Set
	if nilSet.Active() || nilSet.AffectedKey("WAT247") != "" {
		t.Fatal("nil set must be inert")
	}
}

// Publish must create once, then diff-update the SAME dictionary in place —
// the account can't delete dictionaries, so minting new ones would litter it.
func TestPublishCreatesThenDiffs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, r.URL.Path+" "+string(body))
		fmt.Fprintf(w, `{"id":"d1","version_id":"v%d"}`, len(calls))
	}))
	defer srv.Close()
	calls = nil
	c := synth.NewElevenLabs("test-key")
	c.BaseURL = srv.URL

	s := &Set{Name: "t", Rules: map[string]string{"WAT247": "Watt 2 4 7", "DANI": "Danny"}}
	if err := Publish(context.Background(), c, s); err != nil {
		t.Fatal(err)
	}
	if s.DictionaryID != "d1" || s.VersionID != "v1" || s.NeedsPublish() {
		t.Fatalf("create publish state wrong: %+v", s)
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "add-from-rules") {
		t.Fatalf("create must use add-from-rules: %v", calls)
	}

	// Remove one rule, change another: one remove-rules + one add-rules call
	// on the SAME dictionary.
	delete(s.Rules, "DANI")
	s.Rules["WAT247"] = "Watt two four seven"
	calls = nil
	if err := Publish(context.Background(), c, s); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 ||
		!strings.Contains(calls[0], "/d1/remove-rules") || !strings.Contains(calls[0], "DANI") ||
		!strings.Contains(calls[1], "/d1/add-rules") || !strings.Contains(calls[1], "Watt two four seven") {
		t.Fatalf("update must diff against the snapshot: %v", calls)
	}
	if s.VersionID != "v2" || s.NeedsPublish() {
		t.Fatalf("update publish state wrong: %+v", s)
	}

	// No drift → publish-needed is false and nothing would be called.
	if s.NeedsPublish() {
		t.Fatal("clean set must not need publishing")
	}
}

var calls []string
