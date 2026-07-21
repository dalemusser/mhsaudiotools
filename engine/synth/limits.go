package synth

import "strings"

// ConservativeConcurrency is the fallback used when the account's tier can't be
// recognized. It is safe on every paid tier.
const ConservativeConcurrency = 3

// tierConcurrency maps an API `tier` string to ElevenLabs' documented cap on
// simultaneous requests.
//
// Caveat worth knowing: the API does NOT report a concurrency number — `tier` is
// the only signal — so this table is transcribed from ElevenLabs' help docs and
// can drift if they change the limits. Exceeding the cap returns HTTP 429
// "too_many_concurrent_requests", which Synthesize waits out and retries, so a
// stale entry here degrades throughput rather than breaking a run.
//
// The project account reports tier "growing_business", which is not a documented
// public plan name. Its character_limit of 6,000,000 matches the Business plan,
// and Scale and Business share the same cap of 15 — so the value is 15 either
// way, which makes the ambiguity moot.
var tierConcurrency = map[string]int{
	"free":             2,
	"trial":            2,
	"starter":          3,
	"creator":          5,
	"pro":              10,
	"scale":            15,
	"business":         15,
	"growing_business": 15,
}

// MaxConcurrency returns the documented simultaneous-request cap for a tier and
// whether the tier was recognized. Unrecognized tiers (including "enterprise",
// whose cap is negotiated per contract) report ConservativeConcurrency and false,
// letting callers decide whether to trust it or ask the user.
func MaxConcurrency(tier string) (int, bool) {
	if n, ok := tierConcurrency[strings.ToLower(strings.TrimSpace(tier))]; ok {
		return n, true
	}
	return ConservativeConcurrency, false
}
