package voice

import (
	"encoding/json"
	"fmt"
	"os"
)

// LineOverride tweaks a single line's delivery on top of its voice's settings.
// Only the audible per-line knobs are exposed — timbre adherence (similarity,
// speaker boost) stays a per-voice decision.
type LineOverride struct {
	Stability *float64 `json:"stability,omitempty"`
	Style     *float64 `json:"style,omitempty"`
	Speed     *float64 `json:"speed,omitempty"`
}

// Overrides maps a line ID to its delivery tweak. The file lives next to
// voices.json (conventionally voice-overrides.json) and is owned by the audio
// lead — the writers' export never carries these numbers. A player line's
// override applies to every slot's rendering of that line.
type Overrides map[string]LineOverride

// LoadOverrides reads a voice-overrides.json.
func LoadOverrides(path string) (Overrides, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var o Overrides
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("voice: parsing overrides %s: %w", path, err)
	}
	return o, nil
}
