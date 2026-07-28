// Package output decides where synthesized audio lands on disk. The default
// DialogSystem layout matches what the game's Dialogue System consumes: NPC
// files flat at the top level, player files in numbered Player<N>/ folders.
package output

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dalemusser/mhsaudiotools/engine/source"
	"github.com/dalemusser/mhsaudiotools/engine/voice"
)

// Target is one audio file to produce: where it goes under the output root and
// which voice renders it.
type Target struct {
	RelPath   string // slash-separated, relative to the output root
	VoiceID   string
	VoiceName string
	Model     string          // voice's model preference (empty = the run's default)
	Settings  *voice.Settings // voice's expressiveness knobs (nil = defaults)
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
		targets = append(targets, Target{RelPath: rel, VoiceID: ref.VoiceID, VoiceName: ref.VoiceName, Model: ref.Model, Settings: ref.Settings})
	}
	return targets, nil
}

// RunManifestFile describes one audio file of a completed run, for layouts
// whose consumer expects a run-wide manifest.
type RunManifestFile struct {
	Index    int // position in source order
	ID       string
	Speaker  string
	RelPath  string // slash-separated, relative to the output root
	TextHash string // hash of the exact text synthesized
}

// RunManifestWriter is implemented by layouts that emit a consumer-facing
// manifest after a successful run. files covers every non-failed target —
// written this run or already up to date — in source order.
type RunManifestWriter interface {
	WriteRunManifest(outputDir string, files []RunManifestFile) (path string, err error)
}

// BabylonManifest is an alternate layout for the Babylon web projects: audio in
// per-speaker folders, plus the ceremony player's JSON manifest.
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
		targets = append(targets, Target{RelPath: rel, VoiceID: ref.VoiceID, VoiceName: ref.VoiceName, Model: ref.Model, Settings: ref.Settings})
	}
	return targets, nil
}

// ceremonyManifestName matches what the prior Python emitted and the ceremony
// player already loads.
const ceremonyManifestName = "ceremony_audio.json"

// WriteRunManifest emits the ceremony player's manifest, format-compatible with
// prior-apps/audiotools/generate_ceremony_audio.py: each item carries the audio
// URL (prefixed "assets/audio/", the folder the babylon project serves), word
// [startSec, word] pairs for caption sync, the clip duration, and the text
// hash. Words and duration come from the .words.json sidecars, so generate with
// timestamps enabled when the player needs caption data.
func (BabylonManifest) WriteRunManifest(outputDir string, files []RunManifestFile) (string, error) {
	type item struct {
		Index       int      `json:"index"`
		ID          string   `json:"id"`
		Speaker     string   `json:"speaker"`
		Audio       string   `json:"audio"`
		DurationSec float64  `json:"durationSec"`
		Words       [][2]any `json:"words"`
		TextHash    string   `json:"textHash"`
	}
	items := make([]item, 0, len(files))
	for _, f := range files {
		it := item{
			Index:    f.Index,
			ID:       f.ID,
			Speaker:  f.Speaker,
			Audio:    "assets/audio/" + f.RelPath,
			Words:    [][2]any{},
			TextHash: f.TextHash,
		}
		base := strings.TrimSuffix(f.RelPath, path.Ext(f.RelPath))
		if data, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(base+".words.json"))); err == nil {
			var sc struct {
				Words []struct {
					Word  string  `json:"word"`
					Start float64 `json:"start"`
					End   float64 `json:"end"`
				} `json:"words"`
			}
			if json.Unmarshal(data, &sc) == nil && len(sc.Words) > 0 {
				for _, w := range sc.Words {
					it.Words = append(it.Words, [2]any{round3(w.Start), w.Word})
				}
				it.DurationSec = round3(sc.Words[len(sc.Words)-1].End)
			}
		}
		items = append(items, it)
	}

	data, err := json.MarshalIndent(struct {
		Items []item `json:"items"`
	}{items}, "", "  ")
	if err != nil {
		return "", err
	}
	full := filepath.Join(outputDir, ceremonyManifestName)
	if err := os.WriteFile(full, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return full, nil
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// Compile-time assertions.
var (
	_ Layout            = DialogSystem{}
	_ Layout            = BabylonManifest{}
	_ RunManifestWriter = BabylonManifest{}
)
