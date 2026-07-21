package emotion

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultMap seeds the tag map from the direction phrases the scan surfaced in
// the real export ((sighs), (angry), (cheery), …), mapped to v3 audio tags.
// Matching is by exact (lowercased) phrase; a compound like "cheery, over
// loudspeaker" needs its own entry. Teams edit this over time.
func DefaultMap() *Map {
	return &Map{
		Name: "mhs-emotion",
		Tags: map[string]string{
			"sighs":                "[sighs]",
			"sigh":                 "[sighs]",
			"angry":                "[angry]",
			"angrily":              "[angry]",
			"cheery":               "[happy]",
			"cheerily":             "[happy]",
			"brightly":             "[happy]",
			"happy":                "[happy]",
			"getting excited":      "[excited]",
			"excited":              "[excited]",
			"excitedly":            "[excited]",
			"confused":             "[confused]",
			"sarcastic":            "[sarcastic]",
			"sarcastically":        "[sarcastic]",
			"nervous":              "[nervously]",
			"nervously":            "[nervously]",
			"laughs":               "[laughs]",
			"laughing":             "[laughs]",
			"whispers":             "[whispers]",
			"whispering":           "[whispers]",
			"muttering to herself": "[whispers]",
			"muttering":            "[whispers]",
			"sad":                  "[sad]",
			"sadly":                "[sad]",
			"crying":               "[crying]",
			"curious":              "[curious]",
			"curiously":            "[curious]",
			"shouting":             "[shouts]",
			"whispered":            "[whispers]",
		},
		Ignore: []string{
			"calling out from the island",
			"over loudspeaker",
			"in ear",
			"muttering to herself, over loudspeaker",
		},
	}
}

// LoadMap reads a persisted emotion map.
func LoadMap(path string) (*Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Map
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("emotion: parsing %s: %w", path, err)
	}
	if m.Tags == nil {
		m.Tags = map[string]string{}
	}
	return &m, nil
}

// Save writes the map as indented JSON, creating parent directories.
func (m *Map) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
