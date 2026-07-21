package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalemusser/mhsaudiotools/engine/keys"
	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/text"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// resolveAPIKey defers to the engine so the CLI and the desktop app resolve keys
// identically.
func resolveAPIKey(keyFile string) (string, error) {
	return keys.Resolve(keyFile)
}

// loadVoiceConfig accepts either a persisted voices.json or a VoiceAssignments.csv,
// chosen by extension. Prefer the JSON: it is the source of truth for slot
// bindings (see DESIGN.md §6).
func loadVoiceConfig(path string) (*voice.Config, error) {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return voice.LoadAssignmentsCSV(f)
	}
	return voice.LoadConfig(path)
}

// loadLines reads a dialog source, auto-detecting the format unless one is named.
func loadLines(path, adapter string) ([]source.LineItem, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	if adapter == "" || adapter == "auto" {
		lines, name, err := source.ParseAuto(f)
		return lines, name, err
	}
	s, ok := source.Get(adapter)
	if !ok {
		return nil, "", fmt.Errorf("unknown source %q (have: %s)", adapter, strings.Join(sourceNames(), ", "))
	}
	lines, err := s.Parse(f)
	return lines, s.Name(), err
}

func sourceNames() []string {
	var names []string
	for _, s := range source.All() {
		names = append(names, s.Name())
	}
	return names
}

// loadProfile returns the cleanup profile: none, a file, or the built-in MHS one.
func loadProfile(path string, disabled bool) (*text.Profile, error) {
	if disabled {
		return nil, nil
	}
	if path == "" {
		return text.MHSProfile(), nil
	}
	return text.LoadProfile(path)
}

// commas formats n as 1,234,567 — these counts get large enough to misread.
func commas(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
