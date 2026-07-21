package source

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SimpleScript parses the writer/artist "simple script" format: one spoken line
// per row as "ID: text", where ID becomes the audio filename. This is the path
// for dialog that doesn't come from the Dialogue System — a writer's cutscene or
// lesson script. See prior-apps/audiotools/toppo-lessons-070926.txt.
//
// Speaker is left empty in phase 1: these lines carry no speaker, so the caller
// supplies a default voice (the prior Python hardcoded "Toppo").
//
// TODO (phase 2): a formalized variant that also carries an optional speaker
// ("ID | Speaker: text") plus a default voice, for multi-character scripts.
//
// Ported from prior-apps/audiotools/generate_toppo_lessons.py (parse_lessons).
type SimpleScript struct{}

// detectScanLimit bounds how many non-blank lines Detect samples.
const detectScanLimit = 10

func (SimpleScript) Name() string { return "simplescript" }

// Detect samples the leading non-blank lines and reports the format when a
// strong majority look like "ID: text" rows (a single bare-token ID before the
// first colon, then non-empty text). Sampling several lines avoids misfiring on
// ordinary prose that happens to contain a colon.
func (SimpleScript) Detect(sample []byte) bool {
	sc := bufio.NewScanner(strings.NewReader(string(sample)))
	checked, matched := 0, 0
	for sc.Scan() && checked < detectScanLimit {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		checked++
		if looksLikeEntry(line) {
			matched++
		}
	}
	return checked > 0 && matched*100 >= checked*80
}

// looksLikeEntry reports whether line has the "ID: text" shape, where ID is a
// single bare token (no whitespace, no comma — distinguishing it from CSV rows
// and prose).
func looksLikeEntry(line string) bool {
	id, text, ok := strings.Cut(line, ":")
	id = strings.TrimSpace(id)
	return ok && id != "" && !strings.Contains(id, ",") &&
		len(strings.Fields(id)) == 1 && strings.TrimSpace(text) != ""
}

// Parse emits one LineItem per "ID: text" row. It splits on the first colon
// (dialogue text may contain further colons), trims both sides, and skips blank
// lines, lines without a separator, and lines with an empty ID or empty text.
// Duplicate IDs receive a numeric suffix (id_2, id_3, …) so filenames — which
// the consumer matches by ID — stay unique.
func (SimpleScript) Parse(r io.Reader) ([]LineItem, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // tolerate long dialogue lines

	var items []LineItem
	seen := map[string]int{}
	lineno := 0
	for sc.Scan() {
		lineno++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		baseID, text, ok := strings.Cut(line, ":")
		if !ok {
			continue // no separator — not an entry
		}
		baseID = strings.TrimSpace(baseID)
		text = strings.TrimSpace(text)
		if baseID == "" || text == "" {
			continue
		}

		seen[baseID]++
		id := baseID
		if n := seen[baseID]; n > 1 {
			id = fmt.Sprintf("%s_%d", baseID, n)
		}

		items = append(items, LineItem{
			ID:   id,
			Text: text,
			Meta: map[string]string{"line": strconv.Itoa(lineno)},
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("simplescript: reading input: %w", err)
	}
	return items, nil
}

func init() { Register(SimpleScript{}) }
