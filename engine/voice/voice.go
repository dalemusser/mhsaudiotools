// Package voice manages the mapping from characters to ElevenLabs voices,
// including the persisted Player Voice Slots that keep each numbered player
// folder bound to the same voice across regenerations.
package voice

import (
	"fmt"
	"sort"
	"strings"
)

// PlayerCharacter is the speaker name that fans out across player voice slots.
const PlayerCharacter = "Player"

// Assignment binds a (non-player) character to a specific ElevenLabs voice.
// Model selects which ElevenLabs model renders this voice (empty = the run's
// default); it's how a config mixes v2 and v3 voices.
type Assignment struct {
	Character string `json:"character"`
	VoiceID   string `json:"voiceId"`
	VoiceName string `json:"voiceName"`
	Model     string `json:"model,omitempty"`
}

// Slot binds a player voice to a fixed, 1-based slot number. Output for a player
// line is written to "Player<Index>/". The Index→voice binding is persisted so
// Player1 is always the same voice across regenerations.
type Slot struct {
	Index     int    `json:"index"`
	VoiceID   string `json:"voiceId"`
	VoiceName string `json:"voiceName"`
	Model     string `json:"model,omitempty"`
}

// Config is the full per-project voicing configuration. Persisted as JSON, it is
// the source of truth for slot→voice bindings — not the CSV, whose row order
// must never be allowed to silently renumber slots.
type Config struct {
	Assignments []Assignment `json:"assignments"`
	PlayerSlots []Slot       `json:"playerSlots"`

	// Default is the fallback voice for lines whose speaker has no assignment —
	// notably simplescript lines, which carry no speaker at all.
	Default *Assignment `json:"default,omitempty"`
}

// VoiceRef is a resolved voice for one output file. Slot is 0 for a single-voice
// line, or the 1-based player slot number for a player line's fan-out. Model is
// the voice's model preference (empty = the run's default).
type VoiceRef struct {
	Slot      int
	VoiceID   string
	VoiceName string
	Model     string
}

// Voicing is the set of voices one line renders with: exactly one for an NPC
// line, one per player slot for a player line.
type Voicing struct {
	Voices []VoiceRef
}

// Resolve returns the voice(s) for a line. Player lines fan out across the
// configured slots (carrying each slot's index, so the caller can place output
// in the matching Player<N> folder); other lines use their character's
// assignment, falling back to Default when the speaker is unknown or empty.
func (c *Config) Resolve(speaker string, isPlayer bool) (Voicing, error) {
	if isPlayer {
		slots := c.SortedSlots()
		if len(slots) == 0 {
			return Voicing{}, fmt.Errorf("voice: player line but no player voice slots are configured")
		}
		refs := make([]VoiceRef, 0, len(slots))
		for _, s := range slots {
			if s.VoiceID == "" {
				return Voicing{}, fmt.Errorf("voice: player slot %d has no voice assigned", s.Index)
			}
			refs = append(refs, VoiceRef{Slot: s.Index, VoiceID: s.VoiceID, VoiceName: s.VoiceName, Model: s.Model})
		}
		return Voicing{Voices: refs}, nil
	}
	if a, ok := c.VoiceFor(speaker); ok && a.VoiceID != "" {
		return Voicing{Voices: []VoiceRef{{VoiceID: a.VoiceID, VoiceName: a.VoiceName, Model: a.Model}}}, nil
	}
	if c.Default != nil && c.Default.VoiceID != "" {
		return Voicing{Voices: []VoiceRef{{VoiceID: c.Default.VoiceID, VoiceName: c.Default.VoiceName, Model: c.Default.Model}}}, nil
	}
	if speaker == "" {
		return Voicing{}, fmt.Errorf("voice: line has no speaker and no default voice is set")
	}
	return Voicing{}, fmt.Errorf("voice: no voice assigned for speaker %q and no default voice is set", speaker)
}

// VoiceFor returns the voice assigned to a non-player character. Player lines are
// handled via PlayerSlots, not this method.
func (c *Config) VoiceFor(character string) (Assignment, bool) {
	for _, a := range c.Assignments {
		if strings.EqualFold(a.Character, character) {
			return a, true
		}
	}
	return Assignment{}, false
}

// SortedSlots returns a copy of the player slots ordered by Index.
func (c *Config) SortedSlots() []Slot {
	out := append([]Slot(nil), c.PlayerSlots...)
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}
