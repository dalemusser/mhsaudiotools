package pron

import (
	"context"
	"fmt"
	"sort"

	"github.com/dalemusser/mhsaudiotools/engine/synth"
)

// Publish syncs the set's rules to its account dictionary and records the new
// locator + snapshot on the set (the caller saves the file). First publish
// creates the dictionary; later publishes update it in place as an exact
// add/remove diff against the last-published snapshot — never a new dictionary,
// since the account has no way to delete one.
func Publish(ctx context.Context, c *synth.ElevenLabs, s *Set) error {
	if !s.Active() {
		return fmt.Errorf("pron: nothing to publish (no rules)")
	}

	rules := make([]synth.PronunciationRule, 0, len(s.Rules))
	for _, r := range s.SortedRules() {
		rules = append(rules, synth.PronunciationRule{From: r.From, To: r.To})
	}

	var loc synth.DictionaryLocator
	var err error
	if s.DictionaryID == "" {
		loc, err = c.CreatePronunciationDictionary(ctx, s.Name, rules)
		if err != nil {
			return fmt.Errorf("pron: creating dictionary: %w", err)
		}
	} else {
		// Exact diff against the last publish: removals first, then
		// adds/overwrites; the final call's version is the one to reference.
		var removed []string
		for from := range s.Published {
			if _, ok := s.Rules[from]; !ok {
				removed = append(removed, from)
			}
		}
		sort.Strings(removed)
		var changed []synth.PronunciationRule
		for _, r := range s.SortedRules() {
			if s.Published[r.From] != r.To {
				changed = append(changed, synth.PronunciationRule{From: r.From, To: r.To})
			}
		}

		loc = synth.DictionaryLocator{ID: s.DictionaryID, VersionID: s.VersionID}
		if len(removed) > 0 {
			loc, err = c.RemovePronunciationRules(ctx, s.DictionaryID, removed)
			if err != nil {
				return fmt.Errorf("pron: removing rules: %w", err)
			}
		}
		if len(changed) > 0 {
			loc, err = c.AddPronunciationRules(ctx, s.DictionaryID, changed)
			if err != nil {
				return fmt.Errorf("pron: updating rules: %w", err)
			}
		}
	}

	s.DictionaryID, s.VersionID = loc.ID, loc.VersionID
	s.PublishedHash = s.RulesHash()
	s.Published = make(map[string]string, len(s.Rules))
	for f, t := range s.Rules {
		s.Published[f] = t
	}
	return nil
}
