package text

// MHSProfile returns the cleanup rules ported from
// prior-apps/mhs dialogue 061026/UpdateText.py, which the Dialogue System export
// path applies to every line (via Entry.py's constructor).
//
// Order matches the original: all removals run first, then all replacements —
// and within each group the original sequence is preserved, because it is
// load-bearing (en-dash → space must run before hyphen → space, and so on).
//
// Two deliberate, documented deviations from the Python:
//
//  1. ELLIPSES ARE REPLACED WITH A SPACE, NOT DELETED. The original removes "…"
//     and "..." outright, which fuses words when an ellipsis sits between two
//     letters — the live export contains 5 such cases, which today are spoken as
//     "withsome" and "systemsRunning". Replacing with a space plus the trailing
//     whitespace-collapse rule yields text identical to the original everywhere
//     an ellipsis was surrounded by spaces (the overwhelming majority), and fixes
//     the fused words. Note this still DROPS the pause an ellipsis implies; if the
//     writers intend those pauses, keeping the ellipsis is a one-line change here.
//
//  2. FAMILIES OF LITERALS BECAME REGEXES ([em1]…[/em6], the ~30 {{PLACEHOLDER …}}
//     variants, <color=#…>). Besides being far shorter, this catches NEW
//     placeholders the hardcoded list would miss — which otherwise get read aloud
//     to students. The patterns also absorb the malformed variants in the original
//     list (a "{{PLACEHOLDER - PLAYER CLOSES MENU}" with one closing brace, a
//     "[Placeholder - Character Customization]]" with one opening bracket).
func MHSProfile() *Profile {
	return &Profile{
		Name: "mhs-dialogue",
		Rules: []Rule{
			// --- removals ---
			{Kind: RuleRegex, Op: OpRemove, From: `\[/?em[1-6]\]`, Note: "emphasis markup [em1]…[/em6]"},
			{Kind: RuleLiteral, Op: OpRemove, From: `\r`, Note: "literal backslash-r in the export"},
			{Kind: RuleLiteral, Op: OpRemove, From: `\n`, Note: "literal backslash-n in the export"},
			{Kind: RuleLiteral, Op: OpRemove, From: "[/r]"},
			{Kind: RuleLiteral, Op: OpRemove, From: "[/n]"},
			{Kind: RuleLiteral, Op: OpRemove, From: "<joke>"},
			{Kind: RuleLiteral, Op: OpRemove, From: "[var=classifierFeedback]"},
			{Kind: RuleRegex, Op: OpRemove, From: `(?i)\{\{\s*placeholder[^{}]*\}{1,2}`, Note: "{{PLACEHOLDER - …}} stage directions"},
			{Kind: RuleRegex, Op: OpRemove, From: `(?i)\[{1,2}\s*placeholder[^\[\]]*\]{1,2}`, Note: "[[PLACEHOLDER - …]] stage directions"},
			{Kind: RuleLiteral, Op: OpRemove, From: "[nosubtitle]"},
			{Kind: RuleLiteral, Op: OpRemove, From: "[In ear]"},
			{Kind: RuleRegex, Op: OpRemove, From: `(?i)<color=#[0-9a-f]{3,8}>`, Note: "subtitle color markup"},
			{Kind: RuleLiteral, Op: OpRemove, From: "</color>"},
			{Kind: RuleLiteral, Op: OpRemove, From: "(brightly)", Note: "stage direction"},
			{Kind: RuleLiteral, Op: OpRemove, From: "(getting excited)", Note: "stage direction"},
			{Kind: RuleLiteral, Op: OpRemove, From: "(muttering to herself)", Note: "stage direction"},
			{Kind: RuleLiteral, Op: OpRemove, From: "*"},
			{Kind: RuleLiteral, Op: OpRemove, From: `"`},
			{Kind: RuleLiteral, Op: OpRemove, From: "“", Note: "left smart quote"},
			{Kind: RuleLiteral, Op: OpRemove, From: "”", Note: "right smart quote"},

			// Deviation 1: space, not deletion — see the doc comment.
			{Kind: RuleRegex, Op: OpReplace, From: `\.\.\.|\x{2026}`, To: " ", Note: "ellipsis: space so adjacent words don't fuse"},

			// --- replacements (original order is load-bearing) ---
			{Kind: RuleLiteral, Op: OpReplace, From: "’", To: "'", Note: "smart apostrophe"},
			{Kind: RuleLiteral, Op: OpReplace, From: "–", To: " ", Note: "en dash"},
			{Kind: RuleLiteral, Op: OpReplace, From: "-", To: " ", Note: "hyphen"},
			{Kind: RuleLiteral, Op: OpReplace, From: "—", To: " ", Note: "em dash"},
			// Pronunciation fixes moved to the server-side dictionary
			// (engine/pron.DefaultSet): rewriting the text here would change
			// what captions and word timings align to, which is exactly what
			// the dictionary approach exists to avoid.

			// Tidy up the whitespace the rules above leave behind.
			{Kind: RuleRegex, Op: OpReplace, From: `\s+`, To: " ", Note: "collapse whitespace"},
		},
	}
}
