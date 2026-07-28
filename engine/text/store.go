package text

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Profiles are meant to be edited and shared by the team, so they serialize with
// readable names ("regex"/"remove") rather than raw enum numbers.

func (k RuleKind) String() string {
	switch k {
	case RuleLiteral:
		return "literal"
	case RuleRegex:
		return "regex"
	default:
		return "unknown"
	}
}

func (k RuleKind) MarshalJSON() ([]byte, error) { return json.Marshal(k.String()) }

func (k *RuleKind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "literal":
		*k = RuleLiteral
	case "regex":
		*k = RuleRegex
	default:
		return fmt.Errorf("text: unknown rule kind %q (want \"literal\" or \"regex\")", s)
	}
	return nil
}

func (o RuleOp) String() string {
	switch o {
	case OpRemove:
		return "remove"
	case OpReplace:
		return "replace"
	default:
		return "unknown"
	}
}

func (o RuleOp) MarshalJSON() ([]byte, error) { return json.Marshal(o.String()) }

func (o *RuleOp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch s {
	case "remove":
		*o = OpRemove
	case "replace":
		*o = OpReplace
	default:
		return fmt.Errorf("text: unknown rule op %q (want \"remove\" or \"replace\")", s)
	}
	return nil
}

// LoadProfile reads a cleanup profile from disk.
func LoadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("text: parsing %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("text: %s: %w", path, err)
	}
	return &p, nil
}

// Save writes the profile as indented JSON, creating parent directories.
func (p *Profile) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Validate rejects rules that would corrupt output — an empty "from" makes a
// replace insert its text between every character of every line — and compiles
// every regex up front, so a typo surfaces at load time rather than midway
// through a run of thousands of lines.
func (p *Profile) Validate() error {
	for i := range p.Rules {
		if p.Rules[i].From == "" {
			return fmt.Errorf("rule %d has an empty \"from\"", i)
		}
	}
	return p.precompile()
}
