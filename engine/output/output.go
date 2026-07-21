// Package output decides where synthesized audio lands on disk. The default
// DialogSystem layout matches what the game's Dialogue System consumes: NPC
// files flat at the top level, player files in numbered Player<N>/ folders.
package output

import (
	"fmt"

	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// Target is one audio file to produce: where it goes under the output root and
// which voice renders it.
type Target struct {
	RelPath   string // slash-separated, relative to the output root
	VoiceID   string
	VoiceName string
	Model     string // voice's model preference (empty = the run's default)
}

// Layout maps a line and its resolved voicing to the audio files to produce.
type Layout interface {
	Name() string
	Targets(li source.LineItem, v voice.Voicing, ext string) ([]Target, error)
}

// DialogSystem is the default layout, consumed directly by the in-game Dialogue
// System — which finds a line's audio purely by ID = filename. NPC lines are
// written flat at the top level; player lines go to Player<N>/, where N is the
// voice slot index, so a given slot always maps to the same folder and voice
// across regenerations.
type DialogSystem struct{}

func (DialogSystem) Name() string { return "dialog-system" }

// Targets returns "<ID><ext>" at the top level for a single-voice line, or
// "Player<slot>/<ID><ext>" for each slot of a player line. The ID is used
// verbatim — the Dialogue System matches audio to lines by ID.
func (DialogSystem) Targets(li source.LineItem, v voice.Voicing, ext string) ([]Target, error) {
	if len(v.Voices) == 0 {
		return nil, fmt.Errorf("output: line %q has no resolved voices", li.ID)
	}
	targets := make([]Target, 0, len(v.Voices))
	for _, ref := range v.Voices {
		rel := li.ID + ext
		if ref.Slot >= 1 {
			rel = fmt.Sprintf("Player%d/%s%s", ref.Slot, li.ID, ext)
		}
		targets = append(targets, Target{RelPath: rel, VoiceID: ref.VoiceID, VoiceName: ref.VoiceName, Model: ref.Model})
	}
	return targets, nil
}

// BabylonManifest is an alternate layout for the Babylon web projects: audio in
// per-speaker folders, alongside word timings.
//
// TODO (phase 2): emit the JSON manifest the ceremony player consumes.
type BabylonManifest struct{}

func (BabylonManifest) Name() string { return "babylon-manifest" }

func (BabylonManifest) Targets(li source.LineItem, v voice.Voicing, ext string) ([]Target, error) {
	if len(v.Voices) == 0 {
		return nil, fmt.Errorf("output: line %q has no resolved voices", li.ID)
	}
	speaker := li.Speaker
	if speaker == "" {
		speaker = "unknown"
	}
	targets := make([]Target, 0, len(v.Voices))
	for _, ref := range v.Voices {
		rel := fmt.Sprintf("%s/%s%s", speaker, li.ID, ext)
		if ref.Slot >= 1 {
			rel = fmt.Sprintf("%s/Player%d/%s%s", speaker, ref.Slot, li.ID, ext)
		}
		targets = append(targets, Target{RelPath: rel, VoiceID: ref.VoiceID, VoiceName: ref.VoiceName, Model: ref.Model})
	}
	return targets, nil
}

// Compile-time assertions.
var (
	_ Layout = DialogSystem{}
	_ Layout = BabylonManifest{}
)
