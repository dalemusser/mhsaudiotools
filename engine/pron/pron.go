// Package pron manages the project's pronunciation rules and the ElevenLabs
// pronunciation dictionary they publish to. Unlike the cleanup profile's old
// text replacement ("WAT247" → "Watt 2 4 7" in the request text), a dictionary
// fixes pronunciation server-side: the request keeps the writers' spelling, so
// word timings and captions align to the displayed text — "WAT247" becomes one
// timed token spanning the whole spoken expansion, exactly what caption
// highlighting needs. Verified live 2026-07-27: alias rules work on both
// models we render with (docs/pronunciation-dictionaries.md).
package pron

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Set is the project's pronunciation rules plus the publish state of the
// account dictionary that carries them. Persisted as pronunciations.json.
type Set struct {
	Name  string            `json:"name"`
	Rules map[string]string `json:"rules"` // written form -> spoken form (alias)

	// Publish state: which account dictionary version carries these rules.
	// When RulesHash differs from PublishedHash the set has drifted and must
	// be republished before a run. Published snapshots the rules as of the
	// last publish so an update can be expressed as an exact add/remove diff —
	// dictionaries can't be deleted, only versioned in place.
	DictionaryID  string            `json:"dictionaryId,omitempty"`
	VersionID     string            `json:"versionId,omitempty"`
	PublishedHash string            `json:"publishedHash,omitempty"`
	Published     map[string]string `json:"published,omitempty"`
}

// Rule is one alias in publish order.
type Rule struct{ From, To string }

// DefaultSet seeds the rules that used to live in the cleanup profile as text
// replacements (single tokens, so the server's token matching finds them).
func DefaultSet() *Set {
	return &Set{
		Name: "mhs-pronunciations",
		Rules: map[string]string{
			"TK":       "Tea Kay",
			"WAT247":   "Watt 2 4 7",
			"HydroSci": "Hydro Sci",
			"Hydrosci": "Hydro Sci",
			"DANI":     "Danny",
			"Argh":     "ArrGh",
		},
	}
}

// DefaultPath is the shared per-user pronunciations file, used when a project
// doesn't carry its own — the same convention as the job history.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mhsaudio", "pronunciations.json"), nil
}

// Load reads a pronunciations file.
func Load(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Set
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("pron: parsing %s: %w", path, err)
	}
	if s.Rules == nil {
		s.Rules = map[string]string{}
	}
	return &s, nil
}

// LoadOrDefault reads path, seeding the built-in defaults when the file
// doesn't exist yet (it is written on first publish).
func LoadOrDefault(path string) (*Set, error) {
	s, err := Load(path)
	if os.IsNotExist(err) {
		return DefaultSet(), nil
	}
	return s, err
}

// Save writes the set as indented JSON, creating parent directories.
func (s *Set) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Active reports whether the set would change anything at all.
func (s *Set) Active() bool { return s != nil && len(s.Rules) > 0 }

// SortedRules returns the rules in deterministic order for publishing.
func (s *Set) SortedRules() []Rule {
	out := make([]Rule, 0, len(s.Rules))
	for f, t := range s.Rules {
		out = append(out, Rule{From: f, To: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out
}

// RulesHash fingerprints the rule content (not the publish metadata).
func (s *Set) RulesHash() string {
	h := sha1.New()
	for _, r := range s.SortedRules() {
		fmt.Fprintf(h, "%s\x00%s\n", r.From, r.To)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NeedsPublish reports whether the account dictionary is missing or stale for
// these rules. An empty set never needs publishing (nothing to apply).
func (s *Set) NeedsPublish() bool {
	if !s.Active() {
		return false
	}
	return s.DictionaryID == "" || s.VersionID == "" || s.PublishedHash != s.RulesHash()
}

// AffectedKey fingerprints the subset of rules that touch this text — the
// manifest key for pronunciation. It returns "" when no rule word occurs, so
// editing a rule regenerates exactly the lines containing its word, never the
// whole batch. Substring matching deliberately over-approximates the server's
// token matching: the safe failure mode is a rare needless regeneration, never
// a stale file.
func (s *Set) AffectedKey(text string) string {
	if !s.Active() {
		return ""
	}
	var matched []Rule
	for _, r := range s.SortedRules() {
		if strings.Contains(text, r.From) {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		return ""
	}
	h := sha1.New()
	for _, r := range matched {
		fmt.Fprintf(h, "%s\x00%s\n", r.From, r.To)
	}
	return hex.EncodeToString(h.Sum(nil))
}
