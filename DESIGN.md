# mhsaudiotools — Design

Native desktop + CLI tools for generating game dialog audio with ElevenLabs,
built to replace the ad‑hoc Python scripts in `prior-apps/` and to be handed to
non‑programmer teammates (artists, writers, game devs) so they can generate
audio without going through Dale.

This document is the living record of decisions. Code is organized so that both
the desktop app and the CLI are thin shells over one shared engine.

---

## 1. Goals & non‑goals

**Goals**
- One desktop app that a non‑technical teammate can install and run like any
  other app — no Python, no virtualenv, no terminal.
- Support the project's real dialog sources with room to add more.
- Generate audio as a long‑running, monitorable, resumable background job and
  write files straight to a user‑chosen folder on disk.
- Produce output in the exact layout the in‑game Dialogue System consumes, with
  no manual post‑processing.

**Non‑goals (for now)**
- A hosted/multi‑tenant web service (auth, storage, server ops — deliberately avoided).
- A general audio editor. This generates dialog audio; it doesn't edit it.

---

## 2. Platform decisions

| Decision | Choice | Why |
|---|---|---|
| Shell | **Wails v2** | Native app, web‑tech UI, single‑icon distribution. v3 only if a compelling feature justifies it. |
| UI model | **Wails default bindings** (JS → bound Go methods) | Avoids running a localhost HTTP server on user machines (port conflicts / security prompts are hard to support remotely). |
| Frontend | Plain JS over Wails bindings + Tailwind (`plain` template, no npm) | See "no HTMX" below. Tailwind via the standalone CLI, matching the org's existing pattern. |
| Signing | macOS signed + notarized (cert on hand); handle Windows SmartScreen; Linux as bonus | Unsigned apps reintroduce the "scary to run" friction we're removing. |
| Delivery | **CLI and Wails built in parallel**, both over the shared engine | Artists need a UI now; CLI is useful for Dale/automation. Neither is "the app." |
| Repo | One repo, **two Go modules**: engine+CLI, and the Wails app | See "two modules" below. |
| ElevenLabs | **Small in‑house REST client**, no third‑party SDK | Only a few endpoints needed; own the timestamp parsing; no external dependency in a team tool. |
| Secrets | API key from env var or OS keychain, never in source | Replaces the hardcoded key anti‑pattern in `prior-apps`. |

---

## 3. Architecture: one engine, two shells, pluggable edges

```
 SOURCE          NORMALIZE          TRANSFORM         VOICE MAP        SYNTHESIZE         OUTPUT
 (adapters)  →   LineItem[]     →   text cleanup  →   speaker→voice →  ElevenLabs    →   files (+ manifest)
 dbexport CSV     {ID,Speaker,     remove/replace    assignments +    (format, bitrate,  dialog-system layout
 simplescript      Text, Meta}      profiles          player slots     timestamps?,       or babylon-manifest
 (future: …)                       (ordered)                            voice settings)    idempotent/resumable
```

The **engine is the product.** `cmd/cli` and `wails/` are thin presentation
layers that both call into it. A feature added to the engine is immediately
available to both shells — this is what lets them be built in parallel.

The normalized **`LineItem`** in the middle is the pivot: every source adapter's
only job is to emit `{ ID, Speaker, Text, Meta }`. Everything downstream is
written once against `LineItem` and never needs to know the input format.

### Repo layout

```
mhsaudiotools/
  go.mod                     module 1: github.com/dalemusser/mhsaudiotools (engine + CLI)
  DESIGN.md                  this document
  engine/                    the brain — no Wails, no terminal
    source/                  input adapters → []LineItem (dbexport, simplescript, …), registry + auto-detect
    text/                    cleanup profiles (ordered literal/regex remove & replace)
    voice/                   voice assignments + Player Voice Slots
    synth/                   TTS contract + ElevenLabs REST client
    job/                     plan + worker pool, retries, progress, idempotency
    output/                  layout writers: dialog-system, babylon-manifest
    keys/                    API key resolution, shared by both shells
  cmd/cli/                   thin CLI shell over engine
  wails/                     module 2: the desktop app (see wails/README.md)
  prior-apps/                existing Python, kept for reference (do not ship)
```

### Two modules, one repo

Both modules target **Go 1.25.0** (Go 1.25 shipped Aug 2025; well past its first
year and a dozen patch releases — a mature target, and the one Wails pins, so the
whole repo speaks one version). The split isn't about the Go version; it's about
dependencies: Wails pulls in ~30, while the engine and CLI have **zero**
third-party dependencies — a property worth protecting, since the engine is the
part that must stay portable and easy to reason about. So `wails/` is its own
module that `replace`s the engine to `../`.

The cost: `go build ./...` at the root does not cover the app; build it from
`wails/`. That is a good trade — the app is a thin shell, and the engine stays clean.

### No HTMX (and no localhost server)

The original plan was HTMX + Tailwind. Choosing Wails' **default binding model**
(`window.go.main.App.*`) rules HTMX out: HTMX's whole model is issuing HTTP
requests to server URLs, and there is no server here — running one on a user's
machine is precisely the port-conflict/security-prompt support burden this app
exists to avoid. So the frontend is plain JS calling bound methods; it is small
because the engine does the work. **Tailwind is unaffected** and still used, via
the standalone CLI (no npm, no node_modules).

---

## 4. Input sources

Two mechanisms, complementary:

1. **Named adapters** selectable explicitly, registered in `engine/source`.
2. **Auto‑detect** that sniffs a dropped file and picks an adapter, with manual
   override when the guess is wrong.

**`dbexport`** — Dialogue System database export CSV. The plain export and the
`Diff` export share an identical dialogue‑entry block and differ only in the
preamble above it, so the adapter locates the entry block by the
`DialogueEntries` marker (skip 2 header rows, stop at `OutgoingLinks`) rather
than a fixed offset. That single "find where entries begin" behavior is what
lets one adapter consume both variants. (Ported from
`prior-apps/mhs dialogue 061026/VoiceLineGenerator.py`.)

**`simplescript`** — the writer/artist format: one spoken line per row, either
`ID: text` (speaker‑less; the run's default voice) or `ID | Speaker: text`
(multi‑character scripts; `Player` fans out across the slots like any other
source), freely mixed in one file. `ID` becomes the audio filename. The path
for dialog that isn't a Dialogue System export (a writer's cutscene/lesson
script). See `prior-apps/audiotools/toppo-lessons-070926.txt`.

**Babylon web‑app sources** — out of scope for the team app (Dale maintains
those separately), but the `babylon-manifest` output layout keeps that path open.

---

## 5. Text cleanup (editable profiles)

Replaces the hardcoded lists in `prior-apps/.../UpdateText.py` with saved,
shareable **cleanup profiles**.

- Ordered list of **rules** — order is significant (e.g. em‑dash → space must
  run before hyphen → space), preserved exactly.
- Each rule is **literal or regex**, and **remove or replace**. Regex collapses
  the many `{{PLACEHOLDER …}}` literals into one pattern.
- Two purposes: strip formatting/stage directions (`[em1]`, `<color>`,
  `{{PLACEHOLDER …}}`), and fix pronunciation (`WAT247` → `Watt 2 4 7`,
  `TK` → `Tea Kay`, `DANI` → `Danny`).
- Shareable across the team so everyone generates consistent audio. Saved as JSON
  with readable names (`"kind": "regex"`, `"op": "remove"`) and a `note` per rule;
  regexes are compiled at load so a typo fails fast, not 3,000 lines into a run.
- **One shared `cleanup.json`** the team maintains over time (not baked into code):
  the built-in `MHSProfile()` is a *seed* the app can write to a file to start
  from. The CLI takes `-profile`; the app loads a custom file (with a "Save MHS
  defaults…" action to create the shared one).
- **`text.Suggest()` + `mhsaudio scan`** find what the rules miss: they apply the
  profile, then scan the *residue* for markup/stage-direction families (`[…]`,
  `{{…}}`, `<…>`, `(…)`, escapes) the profile doesn't remove — surfacing only
  genuinely new tokens, each with a ready-to-add remove regex, counts, and
  examples. (Scanning the real export immediately turned up unhandled `[TITLE]`,
  `<color=orange>`, `(sighs)`, etc. that the ported profile missed.)

`text.MHSProfile()` is the port of `UpdateText.py`, with two deliberate deviations:

1. **Ellipses are replaced with a space, not deleted.** The Python removed `…`
   and `...` outright, which *fuses words* when an ellipsis sits between two
   letters. The live export has 5 such cases — today's audio literally says
   "withsome" and "systemsRunning". Replacing with a space (plus the trailing
   whitespace-collapse rule) is byte-identical to the old behavior wherever the
   ellipsis was surrounded by spaces (the other ~186 cases) and fixes the 5.
   *Open question for the writers:* this still drops the **pause** an ellipsis
   implies. If those pauses are intentional, keeping the ellipsis is a one-line
   change — but it would alter prosody on every line, so it needs a human call.
2. **Families of literals became regexes** (`[em1]…[/em6]`, the ~30
   `{{PLACEHOLDER …}}` variants, `<color=#…>`). Shorter, and it catches *new*
   placeholders the hardcoded list would miss — which would otherwise be read
   aloud to students. The patterns also absorb the malformed entries in the
   original list (a `}` with one brace, a `[Placeholder …]]` with one bracket).

*Future:* ElevenLabs server‑side **pronunciation dictionaries** could absorb the
pronunciation rules; the portable string‑replace approach ships first.
Verified live and then **built** the same day (`docs/pronunciation-dictionaries.md`):
**alias** rules work on both of our models and keep the original spelling in
alignment/captions — required for text highlighting during dialog — so
pronunciation moved out of the cleanup profile into `engine/pron`: a
pronunciations.json (shared per-user file by default, editable in-app,
CLI `-pronunciations`), auto-published to one account dictionary (created once,
then diff-updated in place via add-rules/remove-rules), attached to every
request, and keyed into the resume manifest per affected line. **Phoneme**
rules are silently ignored on multilingual v2/v3 (turbo/flash only), so the
engine sends alias rules only.

---

## 6. Voice mapping & Player Voice Slots

**Assignments** — character → ElevenLabs voice, imported from
`VoiceAssignments.csv` (`voice.LoadAssignmentsCSV`, tolerating the file's
trailing‑colon headers and falling back to positional columns), editable in‑app,
with a **"fetch voices"** action that pulls the account's voice list so users pick
from a dropdown instead of pasting voice IDs.

**The persisted JSON config — not the CSV — is the source of truth** for
slot→voice bindings. CSV row order is only an initial guess on first import;
trusting it on a re‑import would let a reordered file silently move voices between
`Player<N>` folders, which is precisely the breakage the slot design prevents.
`Config.MergeFrom` folds a re‑imported CSV in safely: pinned voices keep their
slot no matter what row they now occupy, new voices take the lowest free slot,
and slots absent from the import are **kept** (dropping one would punch a hole in
the `Player<N>` sequence and strand every player who picked it — removal stays an
explicit action). `Config.Validate` reports gaps, duplicates, and missing voices
before a run writes thousands of files.

**Resolution** — `voice.Config.Resolve(speaker, isPlayer)` returns a `Voicing`:
exactly one voice for an NPC line, or one per slot (carrying the slot index) for
a player line. Lines whose speaker has no assignment — notably `simplescript`
lines, which carry no speaker — fall back to `Config.Default`.

**Player Voice Slots** — the key automation. The player character is
customizable: the game offers **6 voice slots**, and a chosen slot must always
map to the same voice, even across audio regenerations. So:

- A single `Player` line **fans out** to one audio file per slot.
- A persisted config pins **slot N → a specific voice** (`Player1` → voice A,
  `Player2` → voice B, …). Every regeneration reads this mapping, so `Player1`
  is always the same voice — eliminating the manual "note on the desk" Dale has
  been keeping. Changing a slot's voice is an explicit, warn‑able action
  ("this invalidates all Player3 audio").
- Slot count is configurable; default **6**.

---

## 7. Output layout

Filenames are **dialog IDs, verbatim** — the Dialogue System matches audio to
lines by ID, so IDs are never altered (IDs containing path separators or `..`
are rejected at plan time — they'd nest, collide, or escape the output folder).
Idempotency keys on `ID` + a manifest entry recording the **text hash, voice,
model, format, and timings flag** — changing any of them regenerates the file.
Legacy text-hash-only manifests still load, matching on text alone (and, for
timings, on whether the sidecar is on disk), so upgrading doesn't re-pay for
existing folders — the accepted tradeoff being that a voice recast made
*before* the upgrade isn't detectable for those entries; a one-time `-force`
clears that. The manifest is flushed periodically during
a run and written atomically, so a crash mid-batch loses at most a few files'
records; a corrupt manifest is a loud error, never a silent full regeneration.

**`dialog-system`** (default) — what the game consumes directly, so no manual
post‑processing (this automates what Dale did by hand before):

```
<output>/
  <npcID>.mp3            all NPC lines, FLAT at top level
  …
  Player1/<playerID>.mp3 player lines, one folder per voice SLOT
  Player2/<playerID>.mp3 (same ID across slots, different voice)
  …
  Player6/<playerID>.mp3
```

A `Layout` maps a line **plus its resolved voicing** to `Target`s
(`{RelPath, VoiceID, VoiceName}`). The `Player<N>` folder number comes from the
**slot index**, not a positional count — that is what guarantees a slot always
lands in the same folder with the same voice.

**`babylon-manifest`** — alternate layout for Dale's Babylon web projects:
per‑speaker folders, plus `ceremony_audio.json` — the manifest the ceremony
player consumes, format‑compatible with the prior Python
(`generate_ceremony_audio.py`): per item the `assets/audio/…` URL,
`[startSec, word]` caption pairs, duration, and text hash. Emitted after every
completed run covering all non‑failed files (an incremental run still describes
the whole set); words/duration are folded in from the `.words.json` sidecars,
so generate with timestamps on when the player needs captions.

When timestamps are requested, word timings are written beside each audio file as
`<id>.words.json`.

Output folder is **user‑selected per job**, with a sensible default
(`~/<AppName>/output/<project>/`).

---

## 8. Synthesis options (per request)

- **Model** — default `eleven_multilingual_v2` (high quality, returns
  character/word timestamps). Not turbo/real‑time.
- **Audio format** — mp3 (native, selectable bitrate) ships first. **WAV** =
  request `pcm_*` and wrap in a RIFF header (Go‑side). **OGG/Opus** may need a
  transcode step or may not be offered on every endpoint — verify against the
  API. *Note:* the with‑timestamps endpoint supports a narrower format set than
  plain TTS, so format × timestamps combinations must be validated.
- **Timestamps on/off per request** — derive word timings from ElevenLabs'
  character alignment. Off for bulk DB‑export runs; on for Babylon captions.
- **Voice settings** (per‑line optional override) — stability, similarity,
  style, speaker‑boost.

---

## 9. Jobs

A generation run is a first‑class **Job**, implemented as plan → execute:

1. **Plan** (cheap, sequential): clean each line's text; drop lines that clean to
   nothing; resolve voicing (player lines fan out across slots); expand to output
   targets; skip targets already up to date.
2. **Execute**: run the remaining targets through a **bounded worker pool**.

- **Idempotent / resumable** — a manifest (`.mhsaudio-manifest.json` in the
  output folder) maps each output file to the SHA‑1 of the text it was generated
  from; a re‑run regenerates only changed/missing files. `Force` redoes
  everything. (The ceremony tool did this; the DB‑export tool did not — now every
  job does.)
- **Monitorable** — `OnProgress` reports total / done / failed as units complete.
- **Fault‑isolated** — one bad line (unknown speaker, API failure) is recorded in
  `Result.Errors` and never stops the batch.
- **Cancellable** — `context` cancellation stops work and still returns a partial
  `Result` plus the manifest of what was written.
- **Manageable** — *phase 2*: persist job records so runs survive an app restart
  and can be listed, resumed, and expired.

### Concurrency: auto-detected from the account tier

Throughput is the whole ballgame: a full DB export is ~2,375 lines but **~5,900
audio files** once player lines fan out across 6 slots. Serial, that is the 1+
hour the team lives with today; at 15 it is minutes. So the default is **run at
the account's full cap**, not a hand-tuned guess.

ElevenLabs caps **simultaneous requests per subscription tier**; exceeding it
returns HTTP 429 `too_many_concurrent_requests`:

| Free | Starter | Creator | Pro | Scale | Business |
|------|---------|---------|-----|-------|----------|
| 2    | 3       | 5       | 10  | 15    | 15       |

**`Options.Concurrency = 0` (the default) auto-detects**: it calls
`GET /v1/user/subscription`, maps `tier` → cap via `synth.MaxConcurrency`, and
uses it. An explicit value overrides and skips the lookup. The UI should present
a selector from **1 to the detected max**, defaulting to max, and show the tier.

Two caveats this design accounts for:

1. **The API does not report a concurrency number** — `tier` is the only signal,
   so the table is transcribed from the help docs and can drift. It is a
   best-effort inference, not an authority. Mitigation: an unrecognized tier
   falls back to `ConservativeConcurrency` (3), a failed lookup never fails the
   run, and a stale entry only costs throughput because 429s are retried.
2. **The key is a shared project account.** Two teammates generating at once each
   request the full cap, so together they exceed it. Mitigation: 429s get a
   larger retry budget than ordinary transient errors (they always clear once
   in-flight requests finish) plus jitter so rejected workers don't retry in
   lockstep. Contention costs time, never failures. The selector lets someone
   dial back if a run is known to overlap.

The project account reports tier **`growing_business`** — not a documented public
plan name. Its 6,000,000 character limit matches the Business plan, and Scale and
Business share the same cap, so the answer is **15** either way. Verified live:
`tier="growing_business"` → `maxConcurrency=15`.

---

## 10. The CLI

`cmd/cli` is a thin shell over the engine — the same engine the Wails app will
call. It exists for Dale/automation and to prove the engine end‑to‑end before the
UI exists; artists get the desktop app, not this.

```
mhsaudio account                     # tier + max concurrency
mhsaudio voices [-filter tera]       # the account's voices, for picking IDs
mhsaudio import-voices -csv VoiceAssignments.csv -out voices.json
mhsaudio generate -in <source> -voices voices.json -out <dir> [-dry-run]
mhsaudio jobs                        # recent runs (history shared with the app)
```

`generate` flags: `-source` (auto|dbexport|simplescript), `-layout`, `-format`,
`-timestamps`, `-concurrency` (0 = auto), `-force`, `-profile` / `-no-cleanup`,
`-model` (v2|v3|full ID — default for voices that don't set one), `-emotion` /
`-emotion-map` (v3 audio tags from the writers' directions),
`-default-speaker` / `-default-voice` (for speaker‑less simplescript lines), `-v`.

- **`-dry-run` needs no API key** and makes no calls (`job.Runner.Plan`). It
  reports lines/files/characters, a **files‑per‑voice breakdown** (the quickest
  way to spot a miscast character before spending an hour), and a sample of the
  cleaned text that will actually be spoken.
- **Ctrl‑C cancels cleanly**: in‑flight work stops, the manifest is saved, and a
  re‑run resumes rather than restarting.
- The API key comes from `$ELEVENLABS_API_KEY`, `-key-file`, or
  `~/.elevenlabs_key` — never source.

## 11. Phasing

**Phase 1 — replace the Python for the team**
- Engine + CLI + Wails UI.
- Sources: `dbexport`, `simplescript`. Cleanup profiles. Voice assignments +
  voice fetch + Player Voice Slots. mp3 output. Timestamps toggle.
- `dialog-system` output layout. Concurrent, resumable, monitorable jobs;
  download/write to chosen folder.

**Phase 2**
- ~~Richer job management (history, expiry)~~ — **built**: per‑run records in
  the user config dir (`jobs.json`, pruned to 50), shared by both shells; the
  app shows a History card (resume/run‑again/open/remove; interrupted runs
  detected at startup), the CLI records runs and lists them via `mhsaudio jobs`.
- ~~Formalized `simplescript` (speaker + default voice)~~ — **built**
  (`ID | Speaker: text`).
- ~~Babylon `ceremony_audio.json` emission~~ — **built** (see §7).
- ~~API key in the OS keychain~~ — **built for macOS** (zero‑dep via the
  system `security` tool; resolution order env → key file → Keychain →
  dotfile; opt‑in via the app's key screen or `mhsaudio key -store-keychain`).
  Windows Credential Manager is a possible follow‑up; Linux stays on the
  dotfile (no universal secret service).
- ~~Per‑line voice‑setting overrides~~ — **built** (approved design, see
  `docs/voice-settings-proposal.md`): per‑voice `settings` in voices.json
  (nil = ElevenLabs defaults; edited via ⚙ in the voice editor, auditioned by
  the ▶ preview, carried across CSV re‑imports), plus per‑line
  `voice-overrides.json` keyed by line ID exposing the audible knobs
  (stability/style/speed; app picker + CLI `-voice-overrides`). Effective
  settings join the resume manifest, so a tweak regenerates exactly the
  affected files.
- WAV / OGG output — deferred: Unity consumes the mp3s directly; revisit only
  if a real need appears.

**Phase 3**
- Expressive `eleven_v3` audio tags ("how a line is said") — **engine built**
  (see below), with UI and CLI exposure since built. Pronunciation dictionaries:
  verified live, then **built** — see §5 note and
  `docs/pronunciation-dictionaries.md`. Phase 3 is complete.

---

## 11a. Version-aware emotion (v3 audio tags)

Verified live: **every voice we use renders on v3**, accepts audio tags, and
returns word timings. The only nuance is that v3 doesn't apply a professional
clone's fine-tuning (`serves_pro_voices=false`), so the 5 pro-clone voices render
at base quality on v3 — a listen-and-decide per voice (see `docs/voice-versions-status.html`).
So model is a **per-voice choice**, not a global switch.

The engine supports it:
- **Per-voice model** — `voice.Assignment.Model` / `voice.Slot.Model` (empty =
  the run's `Options.DefaultModel`, else v2). Threaded through `VoiceRef` → `Target`.
- **`engine/emotion`** — the writers already wrote the direction as parentheticals
  (`(sighs)`, `(angry)`) inline and in the export's `Parenthetical` column
  (`LineItem.Direction`). `emotion.Extract` pulls them out; an editable **tag map**
  (`(sighs)` → `[sighs]`) converts them to v3 audio tags; `DefaultMap` seeds it
  from what the scan surfaced.
- **The job is version-aware per target**: text and model resolve per output file,
  so a v3 voice gets `[tag] text` while a v2 voice gets plain text — including
  across a player line's mixed-model slots. Directions are extracted before
  cleanup (so cleanup can't eat them) and re-applied as tags only for v3.

**UI (built):** the voice editor has a per-voice **model picker** (v2 / v3) with
category-based auto-suggest ("Set models from voice type" → generated = v3,
professional = v2), the ▶ preview honors the chosen model (audition v3 vs v2), and
the generate options have an **"Apply emotion (v3 tags)"** toggle that runs the
built-in tag map. `FetchVoices` now returns each voice's `category`.

The **tag-map editor** is built too: add/edit direction→tag rows, an ignore list,
and a test box showing what a line becomes on v3 — plus the same load/save/"use
defaults"/"save defaults" controls as cleanup. The emotion UI is complete.

**CLI (built):** the terminal path matches the app — `-model` sets the default
model (v2/v3 shorthand or a full ID; per-voice models in the voices file still
win), `-emotion` applies the built-in tag map, `-emotion-map` loads a custom one
(and implies `-emotion`, mirroring `-profile`). The run header reports the
v2/v3 voice mix and warns when `-emotion` is set but no voice renders on v3;
the dry-run's files-per-voice breakdown shows each voice's model.

---

## 12. Known follow‑ups / housekeeping

- **Rotate the ElevenLabs API key** hardcoded in
  `prior-apps/mhs dialogue 061026/VoiceLineGenerator.py` and stored in
  `prior-apps/audiotools/secrets.env` — it is exposed.
- Confirm the `dbexport` entry‑tag → filename convention against a fresh export
  before porting parse logic.
