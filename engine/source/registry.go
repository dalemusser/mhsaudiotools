package source

import (
	"bytes"
	"fmt"
	"io"
	"sort"
)

var registry = map[string]Source{}

// Register adds an adapter to the global registry. Adapters call this from init.
func Register(s Source) { registry[s.Name()] = s }

// Get returns a registered adapter by name.
func Get(name string) (Source, bool) { s, ok := registry[name]; return s, ok }

// All returns every registered adapter, ordered by name.
func All() []Source {
	out := make([]Source, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Detect returns the first adapter (in name order) whose Detect matches sample.
func Detect(sample []byte) (Source, bool) {
	for _, s := range All() {
		if s.Detect(sample) {
			return s, true
		}
	}
	return nil, false
}

// ParseAuto reads r fully, auto-detects the format, and parses it. It returns
// the parsed lines and the name of the adapter that was used.
func ParseAuto(r io.Reader) ([]LineItem, string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", err
	}
	s, ok := Detect(data)
	if !ok {
		return nil, "", fmt.Errorf("source: could not auto-detect input format")
	}
	lines, err := s.Parse(bytes.NewReader(data))
	return lines, s.Name(), err
}
