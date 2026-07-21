// Package emotion turns the stage directions the writers already wrote — the
// parentheticals inside dialogue like "(sighs)" and "(angry)", plus the Dialogue
// System's structured Parenthetical field — into ElevenLabs v3 audio tags such as
// "[sighs]". On v2 those directions are simply dropped (v2 can't act on them), so
// the same source produces expressive audio on v3 and clean audio on v2.
package emotion

import (
	"regexp"
	"strings"
)

// parenRe matches a parenthetical span, e.g. "(muttering to herself)".
var parenRe = regexp.MustCompile(`\(([^()]*)\)`)

// Extract pulls parenthetical stage directions out of a line, returning the text
// with them removed and the list of direction phrases (inner text, trimmed). It
// leaves everything else untouched — cleanup runs on the returned text.
func Extract(text string) (clean string, directions []string) {
	for _, m := range parenRe.FindAllStringSubmatch(text, -1) {
		if d := strings.TrimSpace(m[1]); d != "" {
			directions = append(directions, d)
		}
	}
	clean = parenRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(clean, " ")), directions
}

var spaceRe = regexp.MustCompile(`\s+`)

// Map turns a direction phrase into a v3 audio tag. It is editable and shareable
// like the cleanup profile: a curated phrase→tag table, plus an ignore list for
// directions that aren't emotions (blocking, camera notes) so they're dropped
// rather than voiced or mis-tagged.
type Map struct {
	Name   string            `json:"name"`
	Tags   map[string]string `json:"tags"`             // normalized phrase -> tag, e.g. "sighs" -> "[sighs]"
	Ignore []string          `json:"ignore,omitempty"` // normalized phrases to drop without a tag
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// TagsFor maps direction phrases to audio tags, in order, skipping unknown and
// ignored phrases and de-duplicating. Phrases not in the map are left out (they'd
// otherwise be guesses) — run a scan/preview to decide what to add.
func (m *Map) TagsFor(directions []string) []string {
	if m == nil {
		return nil
	}
	ignore := make(map[string]bool, len(m.Ignore))
	for _, ig := range m.Ignore {
		ignore[normalize(ig)] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range directions {
		n := normalize(d)
		if n == "" || ignore[n] {
			continue
		}
		tag, ok := m.Tags[n]
		if !ok || tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// Apply prefixes the given text with the audio tags for the directions — the
// form v3 expects, where leading tags color the delivery of what follows. If
// there are no mapped tags it returns the text unchanged.
func (m *Map) Apply(text string, directions []string) string {
	tags := m.TagsFor(directions)
	if len(tags) == 0 {
		return text
	}
	return strings.Join(tags, " ") + " " + text
}
