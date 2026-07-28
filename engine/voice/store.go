package voice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadConfig reads a persisted voicing config. The persisted file — not the CSV —
// is the source of truth for slot→voice bindings.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("voice: parsing %s: %w", path, err)
	}
	return &c, nil
}

// Save writes the config as indented JSON, creating parent directories.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// MergeFrom folds a freshly imported config (typically from a CSV) into c while
// preserving the guarantee that a player voice keeps its slot number forever.
//
// Rules:
//   - Character assignments are replaced wholesale; they aren't position-sensitive.
//     But a character keeping the same voice also keeps its Model (v2/v3) choice —
//     the CSV has no model column, and losing a by-ear per-voice decision to a
//     routine re-import would be silent data loss. A character whose voice
//     changed gets a fresh (empty) model: a new voice needs a new by-ear call.
//   - A player voice already pinned to a slot KEEPS that slot number, whatever
//     row it now occupies in the CSV (its display name is refreshed).
//   - A player voice not seen before is assigned the lowest free slot number.
//   - Slots absent from the import are LEFT IN PLACE. Dropping one would punch a
//     hole in the Player<N> sequence and strand every player who picked that
//     slot, so removal stays a deliberate, explicit action — never a side effect
//     of re-importing a file.
//
// It reports the slots it added, so a UI can tell the user what changed.
func (c *Config) MergeFrom(imported *Config) (added []Slot) {
	if imported == nil {
		return nil
	}
	prev := c.Assignments
	c.Assignments = imported.Assignments
	for i := range c.Assignments {
		a := &c.Assignments[i]
		if a.Model != "" {
			continue // the import carried its own choice
		}
		for _, old := range prev {
			if strings.EqualFold(old.Character, a.Character) && old.VoiceID == a.VoiceID {
				a.Model = old.Model
				break
			}
		}
	}

	pinned := make(map[string]int, len(c.PlayerSlots)) // voiceID -> slot index
	used := make(map[int]bool, len(c.PlayerSlots))
	for _, s := range c.PlayerSlots {
		pinned[s.VoiceID] = s.Index
		used[s.Index] = true
	}

	slots := append([]Slot(nil), c.PlayerSlots...)
	for _, in := range imported.PlayerSlots {
		if idx, ok := pinned[in.VoiceID]; ok {
			// Already pinned — keep the slot, refresh the display name.
			for i := range slots {
				if slots[i].Index == idx {
					slots[i].VoiceName = in.VoiceName
				}
			}
			continue
		}
		s := Slot{Index: nextFreeSlot(used), VoiceID: in.VoiceID, VoiceName: in.VoiceName}
		used[s.Index] = true
		pinned[s.VoiceID] = s.Index
		slots = append(slots, s)
		added = append(added, s)
	}

	sort.Slice(slots, func(i, j int) bool { return slots[i].Index < slots[j].Index })
	c.PlayerSlots = slots
	return added
}

func nextFreeSlot(used map[int]bool) int {
	for i := 1; ; i++ {
		if !used[i] {
			return i
		}
	}
}

// Validate reports problems that would break a generation run or the game's
// expectations, so the UI can surface them before thousands of files are written.
func (c *Config) Validate() []string {
	var problems []string

	seen := map[int]bool{}
	for _, s := range c.SortedSlots() {
		if s.Index < 1 {
			problems = append(problems, fmt.Sprintf("player slot index %d is not 1-based", s.Index))
		}
		if seen[s.Index] {
			problems = append(problems, fmt.Sprintf("player slot %d is defined more than once", s.Index))
		}
		seen[s.Index] = true
		if s.VoiceID == "" {
			problems = append(problems, fmt.Sprintf("player slot %d has no voice assigned", s.Index))
		}
	}
	// The game offers a contiguous list of player voice choices, so a gap would
	// strand whoever picked that slot.
	for i := 1; i <= len(c.PlayerSlots); i++ {
		if !seen[i] {
			problems = append(problems, fmt.Sprintf("player slots are not contiguous: Player%d is missing", i))
		}
	}

	byChar := map[string]bool{}
	for _, a := range c.Assignments {
		if a.VoiceID == "" {
			problems = append(problems, fmt.Sprintf("character %q has no voice assigned", a.Character))
		}
		key := strings.ToLower(a.Character)
		if byChar[key] {
			problems = append(problems, fmt.Sprintf("character %q is assigned more than once", a.Character))
		}
		byChar[key] = true
	}
	return problems
}
