# MHS Ceremony — ElevenLabs audio + word-timing generator

## What we're building
A Python tool (run from this `audiotools/` folder) that, for every spoken line in the
end-of-game ceremony, calls **ElevenLabs** to produce:
1. a **voice clip** (MP3) in the speaking character's assigned voice, and
2. **word-level timestamps** (`[[startSeconds, "word"], …]`),

then writes both into the Babylon ceremony so the on-screen caption can be **karaoke-
highlighted in sync with the real audio** (today it fakes the timing with even spacing).

You are writing this tool. Read this whole file first, then propose a short plan before coding.

---

## Environment
- A Python **venv already exists at `audiotools/.env`** (Python 3.14). Use it — do **not** create another.
  - Activate: `source .env/bin/activate`  (or call `.env/bin/python` directly).
- The **`elevenlabs` SDK is already installed** in the venv — no install step needed. Still write a
  pinned `requirements.txt` for reproducibility (capture versions with `.env/bin/pip freeze`).
  (`csv`, `json`, `pathlib`, `argparse`, `hashlib` are stdlib.)
- **API key:** Dale will provide an ElevenLabs key. Read it from the **environment variable
  `ELEVENLABS_API_KEY`**. ⚠️ Do **not** put it in a file named `.env` — that name is the venv
  directory here. If you want a file-based fallback, use a differently-named, git-ignored file
  (e.g. `secrets.env` or `~/.elevenlabs_key`). Never hardcode or commit the key.

---

## Inputs

### 1. Voice assignments — `audiotools/VoiceAssignments.csv`
Columns: `Character:, Voice ID:, Voice Name:`. The five ceremony characters and their voice IDs:
`Toppo, Tera, Anderson, Aryn, Jasper` (the CSV also lists others — DANI, Mission Control, etc.
— ignore those for the ceremony). Match a beat's `speaker` to the `Character:` column to get the `Voice ID:`.

### 2. The lines — `../babylon/ceremony-script.js`
(absolute: `/Users/dale/Documents/Projects/webgl-game-dev/babylon/ceremony-script.js`)
It's **JavaScript**, not JSON: it assigns `window.CEREMONY_SCRIPT = { layout, beats:[…] }`.
Each beat that has a `speaker` and `text` is a spoken line, e.g.:
```js
{ speaker: 'Toppo', expression: 'proud', gesture: 'welcome',
  holo: {…}, text: "Cadet — you made it. …" }
```
Beats with `type: 'celebration'` (and any without `text`) are **not** spoken — skip them.
**Extract the beats robustly** — don't hand-parse with fragile regex. Easiest reliable way:
shell out to Node to emit JSON, e.g.
`node -e "global.window={};require('./script.js');process.stdout.write(JSON.stringify(window.CEREMONY_SCRIPT))"`
(point it at the real path). Confirm Node is available; if not, fall back to a careful parse.

### 3. The key — `ELEVENLABS_API_KEY` env var (see Environment).

---

## ElevenLabs
- Use the **text-to-speech *with timestamps*** capability (current SDK: a `convert_with_timestamps`-
  style method on the TTS client; raw REST: `POST /v1/text-to-speech/{voice_id}/with-timestamps`).
  It returns the audio **plus a character-level alignment** (`characters`,
  `character_start_times_seconds`, `character_end_times_seconds`).
- **Derive word timings from the character alignment:** walk the characters, split on whitespace,
  and set each word's start = the start time of its first character. Output `[[startSec, "word"], …]`,
  with `word` matching how the caption splits text (the ceremony does `text.trim().split(/\s+/)` —
  match that tokenization so word counts line up).
- **Model: ElevenLabs' highest-quality model** — `eleven_multilingual_v2` (the high-quality model that
  returns the character/word timestamps we need). Do **not** use a turbo / real-time model — quality
  matters, latency does not. If a newer model is higher quality *and* still returns timestamps, prefer
  it, but word timings are non-negotiable, so confirm timestamp support before switching.
- `output_format`: highest the account allows — try `mp3_44100_192`, fall back to `mp3_44100_128`.
- Verify exact SDK method/param names against the installed `elevenlabs` version and current docs first.

---

## Outputs (write into the Babylon project so the ceremony can serve them)
Base dir: `../babylon/assets/audio/` (create it).
- One MP3 per spoken beat, stable name by beat order + speaker, e.g. `ceremony_00_Toppo.mp3`.
- A manifest `../babylon/assets/audio/ceremony_audio.json` the ceremony can load — array of items,
  one per spoken beat, e.g.:
  ```json
  { "items": [
    { "index": 0, "speaker": "Toppo",
      "audio": "assets/audio/ceremony_00_Toppo.mp3",
      "durationSec": 14.2,
      "words": [[0.00,"Cadet"],[0.46,"—"],[0.71,"you"], "…"],
      "textHash": "sha1-of-the-source-text" } ] }
  ```
  `index` = the beat's position in `CEREMONY_SCRIPT.beats` (so the player can match by index);
  include `speaker` + `textHash` so drift between the script and the audio is detectable.
- These `audio` + `words` fields mirror the ceremony beat schema (see the header comment in
  `ceremony-script.js`), so wiring the player to use them is a small follow-up (done on the
  Babylon side, not here) — just produce the manifest in that shape.

---

## Behavior / quality bar
- **Generate EVERY spoken line** — every beat with `speaker` + `text`, **including the closing Toppo
  line** and any spoken beats after the celebration. Read the full `beats[]`; don't stop at the first few.
- **Idempotent:** skip a line whose audio already exists **and** whose `textHash` matches; regenerate
  only changed/missing lines. A `--force` flag re-does everything.
- **`--dry-run`** that lists what *would* be generated (lines, speaker, voice ID, char count) without
  calling the API — a quick check that the right lines + voices were extracted. **Credits are ample,
  so this is a sanity check, not a spend gate** — don't block on cost approval before generating.
- Handle API errors/rate limits gracefully (retry with backoff; clear message if the key is missing
  or a voice ID is unknown).
- Print a clear summary: N lines, M generated / K skipped, total characters, output paths.
- Keep it a single well-structured script (suggest `generate_ceremony_audio.py`) with `argparse`.

## Decisions already made (Dale) — don't re-ask these
- **Model:** highest quality (`eleven_multilingual_v2`), **not** real-time/turbo.
- **Scope:** generate **all** spoken lines, **including the closing Toppo line** and any spoken beats
  after the celebration.
- The `elevenlabs` SDK is already installed in the venv.

## First step
Sketch a short plan (file layout + the extract → synthesize → manifest flow + the dry-run), then build it.
Glance at `--dry-run` to confirm the right lines/voices were extracted, then generate them all —
**credits are not a constraint**, so no cost gate.
