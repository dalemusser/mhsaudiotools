package voice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const teamCSV = `Character:,Voice ID:,Voice Name:
Anderson,tG5bw06DXQnuqEU3nPAq,Gwen
Toppo,zadbHdSgYi0Gxv83seiu,Amy
Automated System,GBv7mTt0atIp3Br8iCZE,Thomas
Player,3XOBzXhnDY98yeWQ3GdM,Brayden
Player,PoHUWWWMHFrA8z7Q88pu,Miranda
Player,z5RTzOyJdFDd9rah0KmY,Yomiee
`

func TestLoadAssignmentsCSV(t *testing.T) {
	cfg, err := LoadAssignmentsCSV(strings.NewReader(teamCSV))
	if err != nil {
		t.Fatalf("LoadAssignmentsCSV: %v", err)
	}

	if len(cfg.Assignments) != 3 {
		t.Fatalf("got %d assignments, want 3: %+v", len(cfg.Assignments), cfg.Assignments)
	}
	// Multi-word characters must survive, since dbexport derives "Automated System".
	if a, ok := cfg.VoiceFor("Automated System"); !ok || a.VoiceID != "GBv7mTt0atIp3Br8iCZE" {
		t.Errorf("VoiceFor(Automated System) = %+v, %v", a, ok)
	}
	if a, ok := cfg.VoiceFor("toppo"); !ok || a.VoiceName != "Amy" {
		t.Errorf("VoiceFor is not case-insensitive: %+v, %v", a, ok)
	}

	// Player rows become numbered slots, in file order, and never land in Assignments.
	if _, ok := cfg.VoiceFor("Player"); ok {
		t.Error("Player must not be a character assignment; it belongs in PlayerSlots")
	}
	want := []Slot{
		{Index: 1, VoiceID: "3XOBzXhnDY98yeWQ3GdM", VoiceName: "Brayden"},
		{Index: 2, VoiceID: "PoHUWWWMHFrA8z7Q88pu", VoiceName: "Miranda"},
		{Index: 3, VoiceID: "z5RTzOyJdFDd9rah0KmY", VoiceName: "Yomiee"},
	}
	if len(cfg.PlayerSlots) != len(want) {
		t.Fatalf("got %d slots, want %d", len(cfg.PlayerSlots), len(want))
	}
	for i, w := range want {
		if cfg.PlayerSlots[i] != w {
			t.Errorf("slot %d = %+v, want %+v", i, cfg.PlayerSlots[i], w)
		}
	}
}

func TestLoadAssignmentsCSVNoHeader(t *testing.T) {
	// Positional fallback: first row is data, not a header.
	cfg, err := LoadAssignmentsCSV(strings.NewReader("Toppo,v1,Amy\nPlayer,v2,Brayden\n"))
	if err != nil {
		t.Fatalf("LoadAssignmentsCSV: %v", err)
	}
	if len(cfg.Assignments) != 1 || cfg.Assignments[0].Character != "Toppo" {
		t.Errorf("assignments = %+v, want Toppo kept as data", cfg.Assignments)
	}
	if len(cfg.PlayerSlots) != 1 {
		t.Errorf("slots = %+v, want 1", cfg.PlayerSlots)
	}
}

// The whole point of the slot design: re-importing a REORDERED csv must not move
// a voice to a different Player<N> folder.
func TestMergeFromPreservesSlotBindings(t *testing.T) {
	established, err := LoadAssignmentsCSV(strings.NewReader(teamCSV))
	if err != nil {
		t.Fatal(err)
	}

	// The same voices, reordered, with one renamed and one new voice appended.
	reordered := `Character:,Voice ID:,Voice Name:
Toppo,zadbHdSgYi0Gxv83seiu,Amy
Player,z5RTzOyJdFDd9rah0KmY,Yomiee
Player,3XOBzXhnDY98yeWQ3GdM,Brayden Renamed
Player,PoHUWWWMHFrA8z7Q88pu,Miranda
Player,NEWVOICE00000000000,Joey
`
	imported, err := LoadAssignmentsCSV(strings.NewReader(reordered))
	if err != nil {
		t.Fatal(err)
	}
	added := established.MergeFrom(imported)

	// Brayden was slot 1 and must STILL be slot 1 despite now being CSV row 3.
	bySlot := map[int]Slot{}
	for _, s := range established.PlayerSlots {
		bySlot[s.Index] = s
	}
	if got := bySlot[1].VoiceID; got != "3XOBzXhnDY98yeWQ3GdM" {
		t.Errorf("Player1 voice = %q, want Brayden's — reordering the CSV must not move slots", got)
	}
	if got := bySlot[2].VoiceID; got != "PoHUWWWMHFrA8z7Q88pu" {
		t.Errorf("Player2 voice = %q, want Miranda's", got)
	}
	if got := bySlot[3].VoiceID; got != "z5RTzOyJdFDd9rah0KmY" {
		t.Errorf("Player3 voice = %q, want Yomiee's", got)
	}
	// A renamed voice keeps its slot but refreshes its display name.
	if got := bySlot[1].VoiceName; got != "Brayden Renamed" {
		t.Errorf("Player1 name = %q, want the refreshed name", got)
	}
	// The new voice takes the next free slot and is reported.
	if got := bySlot[4].VoiceID; got != "NEWVOICE00000000000" {
		t.Errorf("Player4 voice = %q, want the new voice", got)
	}
	if len(added) != 1 || added[0].Index != 4 {
		t.Errorf("added = %+v, want just slot 4", added)
	}
}

// A voice dropped from the CSV must not silently punch a hole in Player<N>.
func TestMergeFromKeepsSlotsMissingFromImport(t *testing.T) {
	established, _ := LoadAssignmentsCSV(strings.NewReader(teamCSV))
	shrunk := `Character:,Voice ID:,Voice Name:
Player,3XOBzXhnDY98yeWQ3GdM,Brayden
`
	imported, _ := LoadAssignmentsCSV(strings.NewReader(shrunk))
	established.MergeFrom(imported)

	if len(established.PlayerSlots) != 3 {
		t.Fatalf("slots = %+v, want all 3 retained", established.PlayerSlots)
	}
	if p := established.Validate(); len(p) != 0 {
		t.Errorf("Validate = %v, want no problems", p)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	cfg, _ := LoadAssignmentsCSV(strings.NewReader(teamCSV))
	cfg.Default = &Assignment{VoiceID: "v-default", VoiceName: "Amy"}

	path := filepath.Join(t.TempDir(), "nested", "voices.json")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(got.Assignments) != len(cfg.Assignments) || len(got.PlayerSlots) != len(cfg.PlayerSlots) {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.Default == nil || got.Default.VoiceID != "v-default" {
		t.Errorf("default = %+v, want preserved", got.Default)
	}
	v, err := got.Resolve("Toppo", false)
	if err != nil || v.Voices[0].VoiceID != "zadbHdSgYi0Gxv83seiu" {
		t.Errorf("Resolve after round trip = %+v, %v", v, err)
	}
}

func TestValidateCatchesGaps(t *testing.T) {
	c := &Config{PlayerSlots: []Slot{
		{Index: 1, VoiceID: "a"},
		{Index: 3, VoiceID: "b"}, // gap at 2
	}}
	problems := c.Validate()
	if len(problems) == 0 {
		t.Fatal("expected a problem for the missing Player2")
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "Player2") {
			found = true
		}
	}
	if !found {
		t.Errorf("problems = %v, want one naming Player2", problems)
	}
}

// A routine CSV re-import must not wipe per-character model (v2/v3) choices —
// the CSV has no model column. A character whose voice changed gets a fresh
// (empty) model on purpose: a new voice needs a new by-ear decision.
func TestMergeFromKeepsCharacterModels(t *testing.T) {
	cfg := &Config{
		Assignments: []Assignment{
			{Character: "Toppo", VoiceID: "v-toppo", VoiceName: "Amy", Model: "eleven_v3"},
			{Character: "DANI", VoiceID: "v-dani", VoiceName: "Alex", Model: "eleven_v3"},
		},
	}
	cfg.MergeFrom(&Config{
		Assignments: []Assignment{
			{Character: "toppo", VoiceID: "v-toppo", VoiceName: "Amy 2"},   // same voice, case-folded name
			{Character: "DANI", VoiceID: "v-recast", VoiceName: "Someone"}, // voice changed
		},
	})
	if got := cfg.Assignments[0].Model; got != "eleven_v3" {
		t.Errorf("Toppo model = %q, want eleven_v3 carried forward", got)
	}
	if got := cfg.Assignments[1].Model; got != "" {
		t.Errorf("recast DANI model = %q, want empty (fresh by-ear choice)", got)
	}
}

// A UTF-8 BOM on the header row must not defeat header detection (which would
// turn the header itself into a junk assignment).
func TestAssignmentsCSVStripsBOM(t *testing.T) {
	csv := "\ufeffCharacter:,Voice ID:,Voice Name:\nToppo,v-toppo,Amy\n"
	cfg, err := LoadAssignmentsCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Assignments) != 1 || cfg.Assignments[0].Character != "Toppo" {
		t.Fatalf("BOM broke header detection: %+v", cfg.Assignments)
	}
}

func fptr(v float64) *float64 { return &v }

// Settings survive the voices.json round trip and the CSV re-import carry.
func TestSettingsPersistAndSurviveMerge(t *testing.T) {
	cfg := &Config{Assignments: []Assignment{
		{Character: "Toppo", VoiceID: "v-toppo", VoiceName: "Amy",
			Settings: &Settings{Stability: fptr(0.4), Speed: fptr(1.1)}},
	}}
	path := filepath.Join(t.TempDir(), "voices.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Assignments[0].Settings
	if s == nil || *s.Stability != 0.4 || *s.Speed != 1.1 {
		t.Fatalf("settings lost in round trip: %+v", s)
	}

	got.MergeFrom(&Config{Assignments: []Assignment{
		{Character: "Toppo", VoiceID: "v-toppo", VoiceName: "Amy 2"},
	}})
	if got.Assignments[0].Settings == nil {
		t.Fatal("CSV re-import wiped per-voice settings")
	}
}

func TestLoadOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice-overrides.json")
	data := `{"U1_Toppo_2": {"stability": 0.3, "speed": 1.1}}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := LoadOverrides(path)
	if err != nil {
		t.Fatal(err)
	}
	ov := o["U1_Toppo_2"]
	if ov.Stability == nil || *ov.Stability != 0.3 || ov.Speed == nil || *ov.Speed != 1.1 || ov.Style != nil {
		t.Fatalf("override parsed wrong: %+v", ov)
	}
}
