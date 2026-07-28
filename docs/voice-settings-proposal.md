# Proposal — per-line voice-setting overrides

*Status: **approved and implemented** as recommended (2026-07-27): per-voice
defaults in voices.json (all five knobs, ⚙ in the voice editor), per-line
overrides via `voice-overrides.json` (stability/style/speed), effective
settings keyed into the resume manifest. See docs/USAGE.md "Voice settings &
per-line tweaks".*

## What ElevenLabs lets us set per request

These are the per-request knobs (`voice_settings`) the API accepts; today the
engine sends none and every line uses the voice's account-level defaults:

| Setting | Range | What it does |
|---|---|---|
| `stability` | 0–1 | Low = more emotional variation between takes; high = flat, consistent delivery. On **v3** this effectively snaps to three modes (Creative ≈ 0, Natural ≈ 0.5, Robust ≈ 1). |
| `similarity_boost` | 0–1 | How hard to adhere to the original voice's timbre. |
| `style` | 0–1 | Style exaggeration (v2 family); high values add latency and artifacts. |
| `use_speaker_boost` | bool | Stronger speaker likeness, small latency cost. |
| `speed` | ~0.7–1.2 | Speaking rate. |

## Proposed layering (most specific wins)

1. **Per-voice defaults** — a `settings` object on each assignment/slot in
   `voices.json`, edited in the app's voice editor (sliders + the existing ▶
   preview so it's auditioned, not guessed). This alone likely covers most
   needs ("DANI is always a bit flatter, Toppo a bit livelier").
2. **Per-line overrides** — only where a specific line needs something special.

## Where per-line overrides live — options considered

**A. Sidecar overrides file, keyed by line ID (recommended).**
A `voice-overrides.json` next to `voices.json`:

```json
{
  "U1_Toppo_2":  { "stability": 0.3, "speed": 1.1 },
  "U2_DANI_14":  { "style": 0.6 }
}
```

- The writers' export stays untouched — no risk to the Dialogue System data,
  no writer training needed; the audio lead owns the file.
- Explicit and reviewable (it's in git next to the voices config).
- App: a small editor (line ID picker from the loaded source, sliders, per-line
  audition). CLI: `-voice-overrides file.json`.
- Player lines: the override applies to every slot's rendering of that line.

**B. Inline markup in the dialog text** — e.g. `{{VOICE stability=0.3}}`.
Travels with the line, but pollutes writer-facing text, needs cleanup rules to
strip it, and puts audio-engineering numbers in a writers' tool. Not proposed.

**C. Direction-driven presets** — extend the emotion map so a stage direction
like `(whispers)` can also apply a settings preset (e.g. stability 0.35,
style 0.6) on v2 voices, mirroring how v3 gets audio tags. Zero new authoring
surface and pleasingly symmetric with the emotion system, but coarse (per
direction, not per line) and it entangles two systems. Possible later addition
on top of A, not instead of it.

## Regeneration correctness (important)

Effective settings join the resume manifest entry exactly like voice/model/
format do today: change a line's override (or a voice's defaults) and precisely
the affected files regenerate on the next run — no stale audio, no `-force`.

## Questions before implementing

1. Which knobs matter to you? My suggestion: expose `stability`, `style`, and
   `speed` (the audible ones); leave `similarity_boost`/`speaker_boost` at
   per-voice level only.
2. Is the sidecar-file approach (A) the right call for per-line data?
3. Build order: per-voice defaults first (small, high value), per-line file
   second?
