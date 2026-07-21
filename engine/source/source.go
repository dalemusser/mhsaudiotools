// Package source converts raw dialog input — Dialogue System database exports,
// writer scripts, and future formats — into a normalized slice of LineItems
// that every downstream stage of the engine operates on. Adapters are the only
// code that knows a specific input format; everything after normalization is
// written once against LineItem.
package source

import (
	"io"
	"strings"
)

// PlayerSpeaker is the speaker name that triggers per-slot voice fan-out: a
// single "Player" line is synthesized once per configured player voice slot.
const PlayerSpeaker = "Player"

// LineItem is the normalized unit of work. Its ID becomes the audio filename
// verbatim (the Dialogue System matches audio to lines by ID), so adapters must
// preserve the source ID exactly.
type LineItem struct {
	ID      string            // dialog ID; used verbatim as the output filename
	Speaker string            // character name; PlayerSpeaker triggers fan-out
	Text    string            // raw text as it came from the source (pre-cleanup)
	Meta    map[string]string // optional source-specific extras
}

// IsPlayer reports whether this line is spoken by the customizable player
// character and therefore fans out across the player voice slots.
func (li LineItem) IsPlayer() bool {
	return strings.EqualFold(li.Speaker, PlayerSpeaker)
}

// Source parses one input format into normalized LineItems.
type Source interface {
	// Name is the stable identifier used to select this adapter explicitly.
	Name() string
	// Detect reports whether a leading sample of a file looks like this format;
	// used for auto-detection when the user hasn't chosen an adapter.
	Detect(sample []byte) bool
	// Parse reads the full input and returns normalized lines.
	Parse(r io.Reader) ([]LineItem, error)
}
