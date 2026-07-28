// Package text applies editable, ordered cleanup profiles to raw dialog text
// before synthesis: removing formatting/stage-direction tokens and applying
// pronunciation replacements (e.g. "WAT247" -> "Watt 2 4 7"). Rule order is
// significant and preserved, mirroring prior-apps UpdateText.py.
package text

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// RuleKind selects literal vs. regular-expression matching for a rule.
type RuleKind int

const (
	RuleLiteral RuleKind = iota // From is a literal substring
	RuleRegex                   // From is a regular expression
)

// RuleOp selects removal vs. replacement.
type RuleOp int

const (
	OpRemove  RuleOp = iota // delete every match of From
	OpReplace               // replace every match of From with To
)

// Rule is a single ordered transformation.
type Rule struct {
	Kind RuleKind `json:"kind"`
	Op   RuleOp   `json:"op"`
	From string   `json:"from"`
	To   string   `json:"to,omitempty"` // used only when Op == OpReplace

	// Note describes why the rule exists — these lists get long and inscrutable.
	Note string `json:"note,omitempty"`

	re *regexp.Regexp // compiled cache for RuleRegex
}

// Profile is an ordered, named set of rules, saved per project and shareable
// across the team so everyone generates consistent audio. Order matters.
type Profile struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`

	compileOnce sync.Once
	compileErr  error
}

// precompile compiles every regex rule exactly once up front. One profile can
// be applied from several goroutines at once (the app previews while a run is
// planning), and the rules' lazy compile caches are not safe to fill
// concurrently.
func (p *Profile) precompile() error {
	p.compileOnce.Do(func() {
		for i := range p.Rules {
			if p.Rules[i].Kind != RuleRegex {
				continue
			}
			if _, err := p.Rules[i].compile(); err != nil {
				p.compileErr = fmt.Errorf("rule %d (%q): %w", i, p.Rules[i].From, err)
				return
			}
		}
	})
	return p.compileErr
}

// Apply runs every rule against s in order and returns the cleaned text.
func (p *Profile) Apply(s string) (string, error) {
	if err := p.precompile(); err != nil {
		return "", err
	}
	for i := range p.Rules {
		out, err := p.Rules[i].apply(s)
		if err != nil {
			return "", fmt.Errorf("text: rule %d (%q): %w", i, p.Rules[i].From, err)
		}
		s = out
	}
	return s, nil
}

func (r *Rule) apply(s string) (string, error) {
	repl := ""
	if r.Op == OpReplace {
		repl = r.To
	}
	switch r.Kind {
	case RuleLiteral:
		return strings.ReplaceAll(s, r.From, repl), nil
	case RuleRegex:
		re, err := r.compile()
		if err != nil {
			return "", err
		}
		return re.ReplaceAllString(s, repl), nil
	default:
		return "", fmt.Errorf("unknown rule kind %d", r.Kind)
	}
}

// compile returns the rule's compiled pattern, caching it after the first use.
func (r *Rule) compile() (*regexp.Regexp, error) {
	if r.re != nil {
		return r.re, nil
	}
	re, err := regexp.Compile(r.From)
	if err != nil {
		return nil, err
	}
	r.re = re
	return re, nil
}
