package voice

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// LoadAssignmentsCSV parses a VoiceAssignments.csv — the format the team already
// maintains:
//
//	Character:,Voice ID:,Voice Name:
//	Toppo,zadbHdSgYi0Gxv83seiu,Amy
//	Player,3XOBzXhnDY98yeWQ3GdM,Brayden
//	Player,PoHUWWWMHFrA8z7Q88pu,Miranda
//
// Non-player rows become Assignments. The repeated "Player" rows become player
// voice slots, numbered 1..N in the order they appear.
//
// IMPORTANT: that numbering is only an initial guess for a first import. CSV row
// order must never be trusted to renumber slots on a re-import — a reordered file
// would silently move voices between Player<N> folders, which is exactly the
// breakage the slot design exists to prevent. Use Config.MergeFrom to fold a
// re-imported CSV into an existing config, which preserves established bindings.
func LoadAssignmentsCSV(r io.Reader) (*Config, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("voice: reading assignments CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("voice: assignments CSV is empty")
	}
	// Excel and Notepad save CSVs with a UTF-8 BOM; it rides on the first field
	// and would defeat header detection.
	if len(records[0]) > 0 {
		records[0][0] = strings.TrimPrefix(records[0][0], "\ufeff")
	}

	charCol, idCol, nameCol, start := columns(records[0])

	cfg := &Config{}
	nextSlot := 1
	for _, rec := range records[start:] {
		character := field(rec, charCol)
		voiceID := field(rec, idCol)
		voiceName := field(rec, nameCol)
		if character == "" || voiceID == "" {
			continue // blank or incomplete row
		}
		if strings.EqualFold(character, PlayerCharacter) {
			cfg.PlayerSlots = append(cfg.PlayerSlots, Slot{
				Index: nextSlot, VoiceID: voiceID, VoiceName: voiceName,
			})
			nextSlot++
			continue
		}
		cfg.Assignments = append(cfg.Assignments, Assignment{
			Character: character, VoiceID: voiceID, VoiceName: voiceName,
		})
	}
	if len(cfg.Assignments) == 0 && len(cfg.PlayerSlots) == 0 {
		return nil, fmt.Errorf("voice: no voice assignments found in CSV")
	}
	return cfg, nil
}

// columns locates the character/voice-id/voice-name columns from the header,
// tolerating the trailing colons the team's file uses ("Character:", "Voice ID:").
// If the header isn't recognizable it falls back to positional columns and treats
// the first row as data.
func columns(header []string) (charCol, idCol, nameCol, start int) {
	charCol, idCol, nameCol = -1, -1, -1
	for i, h := range header {
		switch normalizeHeader(h) {
		case "character":
			charCol = i
		case "voice id", "voiceid":
			idCol = i
		case "voice name", "voicename":
			nameCol = i
		}
	}
	if charCol >= 0 && idCol >= 0 {
		if nameCol < 0 {
			nameCol = idCol + 1
		}
		return charCol, idCol, nameCol, 1 // skip the header row
	}
	return 0, 1, 2, 0 // no usable header — assume positional, keep row 0
}

func normalizeHeader(h string) string {
	h = strings.TrimSpace(h)
	h = strings.TrimSuffix(h, ":")
	return strings.ToLower(strings.TrimSpace(h))
}

func field(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}
