package text

import (
	"regexp"
	"sort"
)

// Suggestion is a family of look-alike tokens a scan found in the dialog that a
// cleanup profile doesn't yet handle — a candidate remove rule.
type Suggestion struct {
	Description string   `json:"description"` // human label, e.g. "square-bracket tags [ … ]"
	Pattern     string   `json:"pattern"`     // regex to use as the From of a remove rule
	Count       int      `json:"count"`       // total occurrences across the scanned text
	Examples    []string `json:"examples"`    // distinct sample matches, most frequent first
}

// Rule turns a suggestion into the remove rule the user would add if they accept it.
func (s Suggestion) Rule() Rule {
	return Rule{Kind: RuleRegex, Op: OpRemove, From: s.Pattern, Note: "added from scan: " + s.Description}
}

// detector is one family of markup/stage-direction tokens to look for.
type detector struct {
	desc string
	re   *regexp.Regexp
}

// Ordered most-specific first: doubled delimiters before single ones, so a "{ }"
// detector doesn't re-report the inside of a "{{ }}" already reported above (each
// detector's matches are blanked out before the next one runs).
var detectors = []detector{
	{"double-brace placeholders {{ … }}", regexp.MustCompile(`\{\{[^{}]*\}\}`)},
	{"double-bracket tags [[ … ]]", regexp.MustCompile(`\[\[[^\[\]]*\]\]`)},
	{"brace tags { … }", regexp.MustCompile(`\{[^{}]*\}`)},
	{"square-bracket tags [ … ]", regexp.MustCompile(`\[[^\[\]]*\]`)},
	{"angle tags < … >", regexp.MustCompile(`<[^<>]*>`)},
	{`literal escape sequences (\n, \r, …)`, regexp.MustCompile(`\\[a-zA-Z]`)},
	{"parentheticals ( … ) — may be real speech, review", regexp.MustCompile(`\([^()]*\)`)},
}

// Suggest applies profile to each text, then scans the residue for tokens that
// look like formatting or stage directions the profile doesn't yet remove.
// Because it scans AFTER cleanup, anything the profile already handles never
// reappears — it surfaces only genuinely new strings, which is the "maintain the
// list as we find them" workflow. A nil profile scans the raw text (shows
// everything). Suggestions are returned in detector order; each carries the total
// count and the most frequent distinct examples.
func Suggest(texts []string, profile *Profile) ([]Suggestion, error) {
	work := make([]string, len(texts))
	for i, t := range texts {
		if profile == nil {
			work[i] = t
			continue
		}
		cleaned, err := profile.Apply(t)
		if err != nil {
			return nil, err
		}
		work[i] = cleaned
	}

	var out []Suggestion
	for _, d := range detectors {
		counts := map[string]int{}
		total := 0
		for i, s := range work {
			for _, m := range d.re.FindAllString(s, -1) {
				counts[m]++
				total++
			}
			// Blank this family's spans so looser detectors below don't re-report them.
			work[i] = d.re.ReplaceAllString(s, " ")
		}
		if total == 0 {
			continue
		}
		out = append(out, Suggestion{
			Description: d.desc,
			Pattern:     d.re.String(),
			Count:       total,
			Examples:    topExamples(counts, 10),
		})
	}
	return out, nil
}

// topExamples returns up to n distinct matches, most frequent first (ties broken
// alphabetically for stable output).
func topExamples(counts map[string]int, n int) []string {
	type kv struct {
		s string
		n int
	}
	arr := make([]kv, 0, len(counts))
	for s, c := range counts {
		arr = append(arr, kv{s, c})
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].n != arr[j].n {
			return arr[i].n > arr[j].n
		}
		return arr[i].s < arr[j].s
	})
	out := make([]string, 0, n)
	for _, e := range arr {
		if len(out) == n {
			break
		}
		out = append(out, e.s)
	}
	return out
}
