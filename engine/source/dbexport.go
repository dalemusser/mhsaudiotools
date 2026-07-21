package source

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// DBExport parses Dialogue System database export CSVs. The plain export and the
// "Diff" export share an identical dialogue-entry block and differ only in the
// preamble above it, so this adapter locates the entry block by the
// "DialogueEntries" marker (skip two header rows, stop at "OutgoingLinks")
// rather than a fixed row offset — which lets one adapter handle both variants.
//
// The export is a multi-section CSV (Database, Conversations, DialogueEntries,
// OutgoingLinks, …); section markers are single-field rows and the sections
// have differing column counts, so parsing tolerates a variable field count.
type DBExport struct{}

// Section markers and header layout within the DialogueEntries block.
const (
	sectionDialogueEntries = "DialogueEntries"
	sectionOutgoingLinks   = "OutgoingLinks"
	dialogueHeaderRows     = 2       // the "entrytag,…" and "Text,Number,…" rows
	titleStart             = "START" // conversation START node marker in the Title column
)

// Zero-based columns of a DialogueEntries data row.
// Header: entrytag,ConvID,ID,Actor,Conversant,Title,MenuText,DialogueText,…
const (
	colEntryTag      = 0
	colConvID        = 1
	colID            = 2
	colActor         = 3
	colConversant    = 4
	colTitle         = 5
	colMenuText      = 6
	colDialogueText  = 7
	colParenthetical = 16 // the Dialogue System's structured stage-direction field
)

func (DBExport) Name() string { return "dbexport" }

func (DBExport) Detect(sample []byte) bool {
	return bytes.Contains(sample, []byte(sectionDialogueEntries))
}

// Parse scans to the DialogueEntries marker, skips the two header rows, and
// reads entries until OutgoingLinks, emitting one LineItem per spoken line. The
// entry tag becomes LineItem.ID (used verbatim as the filename) and the speaker
// is derived from the tag (conversationID_speaker_lineID). START nodes and
// entries with empty dialogue text (group/continue nodes) are skipped; "Player"
// speakers fan out downstream via the voice slots, not here.
//
// Ported from prior-apps/mhs dialogue 061026/VoiceLineGenerator.py (readEntries).
func (DBExport) Parse(r io.Reader) ([]LineItem, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // sections have differing column counts
	cr.LazyQuotes = true    // tolerate stray quotes in exported prose

	var (
		items      []LineItem
		inSection  bool
		headerSkip int
	)
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("dbexport: reading CSV: %w", err)
		}
		if len(rec) == 0 {
			continue
		}
		// The BOM rides on the very first field of the file; strip it so the
		// marker comparison is robust regardless of where the section sits.
		first := strings.TrimSpace(strings.TrimPrefix(rec[0], "\ufeff"))

		if !inSection {
			if first == sectionDialogueEntries {
				inSection = true
				headerSkip = dialogueHeaderRows
			}
			continue
		}
		if headerSkip > 0 {
			headerSkip--
			continue
		}
		if first == sectionOutgoingLinks {
			break // end of the DialogueEntries section
		}
		if len(rec) <= colDialogueText {
			continue // not a well-formed entry row
		}
		if strings.TrimSpace(rec[colTitle]) == titleStart {
			continue // conversation START node — not spoken
		}
		if strings.TrimSpace(rec[colDialogueText]) == "" {
			continue // group/continue node with no spoken line
		}
		tag := strings.TrimSpace(rec[colEntryTag])
		if tag == "" {
			continue
		}
		direction := ""
		if len(rec) > colParenthetical {
			direction = strings.TrimSpace(rec[colParenthetical])
		}
		items = append(items, LineItem{
			ID:        tag,
			Speaker:   speakerFromTag(tag),
			Text:      rec[colDialogueText],
			Direction: direction,
			Meta:      map[string]string{"conversation": strings.TrimSpace(rec[colConvID])},
		})
	}
	if !inSection {
		return nil, fmt.Errorf("dbexport: no %q section found", sectionDialogueEntries)
	}
	return items, nil
}

// speakerFromTag derives the speaker from an entry tag of the form
// conversationID_speaker_lineID. A speaker with underscores (e.g.
// "Mission_Control") spans several middle segments, which are rejoined with
// spaces ("Mission Control").
func speakerFromTag(tag string) string {
	parts := strings.Split(tag, "_")
	switch {
	case len(parts) > 3:
		return strings.Join(parts[1:len(parts)-1], " ")
	case len(parts) >= 2:
		return parts[1]
	default:
		return ""
	}
}

func init() { Register(DBExport{}) }
