package source

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SimpleScript parses the writer/artist "simple script" format: one spoken line
// per row, where the ID becomes the audio filename. This is the path for dialog
// that doesn't come from the Dialogue System — a writer's cutscene or lesson
// script. See prior-apps/audiotools/toppo-lessons-070926.txt.
//
// Two line forms, freely mixed in one file:
//
//	ID: text                 speaker-less; voiced by the run's default voice
//	ID | Speaker: text       multi-character scripts; "Player" fans out across
//	                         the player voice slots like any other source
//
// Ported from prior-apps/audiotools/generate_toppo_lessons.py (parse_lessons),
// extended with the speaker variant.
type SimpleScript struct{}

// detectScanLimit bounds how many non-blank lines Detect samples.
const detectScanLimit = 10

func (SimpleScript) Name() string { return "simplescript" }

// Detect samples the leading non-blank lines and reports the format when a
// strong majority look like "ID: text" rows (a single bare-token ID before the
// first colon, then non-empty text). Sampling several lines avoids misfiring on
// ordinary prose that happens to contain a colon.
func (SimpleScript) Detect(sample []byte) bool {
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(string(sample), "\ufeff")))
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

// looksLikeEntry reports whether line has the "ID: text" or "ID | Speaker: text"
// shape, where ID is a single bare token (no whitespace, no comma —
// distinguishing it from CSV rows and prose). A speaker may contain spaces
// ("Mission Control") but not a comma.
func looksLikeEntry(line string) bool {
	head, text, ok := strings.Cut(line, ":")
	if !ok || strings.TrimSpace(text) == "" {
		return false
	}
	id := strings.TrimSpace(head)
	if idPart, speaker, has := strings.Cut(id, "|"); has {
		if strings.TrimSpace(speaker) == "" || strings.Contains(speaker, ",") {
			return false
		}
		id = strings.TrimSpace(idPart)
	}
	return id != "" && !strings.Contains(id, ",") && len(strings.Fields(id)) == 1
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
	used := map[string]bool{} // every ID already taken (incl. literal "x_2" rows)
	count := map[string]int{} // occurrences of each base ID
	lineno := 0
	for sc.Scan() {
		lineno++
		line := sc.Text()
		if lineno == 1 {
			// Excel/Notepad save with a UTF-8 BOM; TrimSpace doesn't remove it,
			// and it would otherwise become an invisible prefix on the first
			// line's ID — a filename the game can never match.
			line = strings.TrimPrefix(line, "\ufeff")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		baseID, text, ok := strings.Cut(line, ":")
		if !ok {
			continue // no separator — not an entry
		}
		baseID = strings.TrimSpace(baseID)
		text = strings.TrimSpace(text)
		speaker := ""
		if idPart, spPart, has := strings.Cut(baseID, "|"); has {
			baseID, speaker = strings.TrimSpace(idPart), strings.TrimSpace(spPart)
		}
		if baseID == "" || text == "" {
			continue
		}

		// Suffix duplicates until the name is genuinely free — "a, a, a_2"
		// must not produce two files named a_2.
		count[baseID]++
		id := baseID
		for n := count[baseID]; used[id]; n++ {
			id = fmt.Sprintf("%s_%d", baseID, n)
		}
		used[id] = true

		items = append(items, LineItem{
			ID:      id,
			Speaker: speaker,
			Text:    text,
			Meta:    map[string]string{"line": strconv.Itoa(lineno)},
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("simplescript: reading input: %w", err)
	}
	return items, nil
}

func init() { Register(SimpleScript{}) }
