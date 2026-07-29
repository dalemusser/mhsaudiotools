# Using the tools

Both tools do the same thing: take **dialog**, apply your **voices**, and write
**audio files** to a folder. This guide covers the app first, then the CLI.

## Concepts (read once)

- **Dialog source** — where the lines come from. Two formats are supported:
  - **Dialogue System export** (`.csv`) — the game's dialog database export. The
    line's ID becomes the audio filename; the speaker is read from the ID.
  - **Writer script** (`.txt`) — one line per row as `id: spoken text`, or
    `id | Speaker: spoken text` for multi-character scripts (`Player` fans out
    across the player slots). Speaker-less lines use the **default voice** you set.
- **Voices** — which ElevenLabs voice speaks each character, kept in a
  `voices.json`. You can import the team's `VoiceAssignments.csv` and the tool
  converts it. The player character has **numbered voice slots** (Player1…Player6);
  each slot stays bound to the same voice across regenerations, so re-running never
  moves a voice to a different `PlayerN` folder.
- **Output layout** — *Dialogue System* (the default) writes NPC files flat at the
  top level and player lines into `Player1/`…`PlayerN/`, exactly as the game loads
  them. *Babylon manifest* is for the web projects.
- **Cleanup** — strips stage directions and formatting (`[em1]`, `{{PLACEHOLDER…}}`)
  before speaking. On by default.
- **Pronunciation** — words like `WAT247` are pronounced correctly by a
  server-side dictionary while the text (and so captions/word timings) keeps the
  writers' spelling. On by default; editable in the app. See
  [Pronunciation](#pronunciation).
- **Voice settings & line tweaks** — per-voice delivery knobs (stability, style,
  speed…) set in the voice editor, plus per-line overrides in a
  `voice-overrides.json`. See
  [Voice settings & per-line tweaks](#voice-settings--per-line-tweaks).
- **Job history** — every run is remembered (app **Recent jobs** card / CLI
  `mhsaudio jobs`); interrupted runs can be resumed. See
  [Job history](#job-history).
- **Model & emotion** — each voice can render on ElevenLabs **v2** (best likeness)
  or **v3** (understands emotion tags). With **Apply emotion** on (CLI: `-emotion`,
  or `-emotion-map` for a custom map), the writers' `(sighs)`/`(angry)` directions
  are pulled out of every line: on **v3** voices they come back as audio tags, on
  **v2** voices they're simply removed from the spoken text (instead of being read
  aloud). Which voices should be v3 is a per-voice, by-ear choice; see the
  [voice v2/v3 reference](voice-versions-status.html). *(CLI: `-model v3` makes v3
  the default for voices that don't set one in the voices file.)*
- **Resume / regenerate** — the tool remembers what it already made (a manifest in
  the output folder). Running again only generates new or changed lines. "Regenerate
  everything" / `-force` redoes them all.

---

## The app

1. **First run** — paste your ElevenLabs API key when asked (see
   [INSTALL.md](INSTALL.md#getting-an-elevenlabs-api-key)). The top bar then shows
   your account and how many requests it can run in parallel.
2. **① Dialog source** — choose your `.csv` export or `.txt` script. Leave format on
   *Detect automatically* unless it guesses wrong.
3. **② Voices** — choose your `voices.json`, or a `VoiceAssignments.csv` to import.
   The panel shows each character's voice and the player slots. Click **Edit…** to
   open the voice editor: **Fetch voices from account** to pick voices by name from
   dropdowns, add/remove characters, and set player slots, then Save. Use **▶** on
   any row to hear that voice speak the preview line before committing.
4. **③ Output** — choose the folder for the audio. Options:
   - **Layout** — leave on *Dialogue System (game)*.
   - **Audio** — MP3 quality.
   - **Default voice** — the character whose voice reads a writer script's lines
     (e.g. `Toppo`); leave blank for Dialogue System exports.
   - **Parallel** — how many lines to generate at once. Defaults to your account's
     maximum, which is fastest. Lower it if a teammate is generating at the same
     time on the same account.
   - **Clean up text** (on), **Word timings** (off), **Regenerate everything** (off).
     Under it, **Edit rules…** opens the cleanup editor: add/reorder/remove
     remove & replace rules, **test a line** to see the result live, and **scan the
     chosen source** to one-click add rules for markup that isn't handled yet.
   - **Apply emotion (v3 tags)** — turns the writers' `(sighs)`/`(angry)` directions
     into v3 audio tags, but only for voices set to **v3** in the voice editor (mark
     them there with **Set models from voice type**, and ▶ to audition v3 vs v2).
     **Edit tags…** opens the tag-map editor (direction → `[tag]`, an ignore list,
     and a test box).
5. **Preview** — shows what it *would* do (lines, files, characters, files-per-voice)
   without calling ElevenLabs or spending anything. Check the per-voice counts look
   right.
6. **Generate** — runs it, with a progress bar. **Stop** cancels; what's done is
   kept, and running again resumes.
7. **Open output folder** — the audio is ready to drop into the game.

---

## The CLI

```
mhsaudio <command> [flags]

  account         show the account tier and its max parallelism
  voices          list the voices available on the account
  import-voices   convert/merge a VoiceAssignments.csv into a voices.json
  scan            find markup the cleanup rules miss; suggest remove rules
  generate        generate audio (use -dry-run to preview)
  version         show the version
```

The API key comes from `$ELEVENLABS_API_KEY`, `-key-file <path>`, or `~/.elevenlabs_key`.
(`scan` and `version` need no key.)

### Set up voices once

```bash
# Convert the team's CSV to a voices.json (preserves player-slot bindings on re-import)
mhsaudio import-voices -csv VoiceAssignments.csv -out voices.json

# Browse available voices to fill in / correct assignments
mhsaudio voices -filter toppo
```

### Maintaining the cleanup rules

Cleanup (removing stage directions/markup) uses a profile. By default it's the
built-in `mhs-dialogue` rules; keep **one shared `cleanup.json`** the team edits
over time. Create it from the defaults (in the app: *Save MHS defaults…*), then
edit it as new markup turns up. Pronunciation fixes belong in the
[pronunciation rules](#pronunciation), not here — a cleanup replacement would
rewrite the text that captions and word timings align to.

Find what the current rules miss — `scan` applies the profile and reports the
leftover markup, so it only surfaces genuinely new tokens:

```bash
mhsaudio scan -in 2026-06-10-MHSDialogueExport.csv                 # against built-in rules
mhsaudio scan -in export.csv -profile cleanup.json                 # against your shared file
```

Each suggestion comes with a ready-to-paste remove regex and example matches.
Add the real markup to `cleanup.json`; leave anything that's actual speech
(parentheticals especially). Then pass your file to `generate`:

```bash
mhsaudio generate -in export.csv -voices voices.json -out ./audio -profile cleanup.json
```

### Preview, then generate

```bash
# Dry run — no API calls, nothing written. Always do this first on a new export.
mhsaudio generate -in 2026-06-10-MHSDialogueExport.csv \
  -voices voices.json -out ./audio -dry-run

# Real run. Parallelism auto-detects the account's max; add -v for per-file logs.
mhsaudio generate -in 2026-06-10-MHSDialogueExport.csv \
  -voices voices.json -out ./audio
```

A writer script needs a default voice for its speaker-less lines; lines can
also name their speaker directly (`ID | Speaker: text`), and both forms mix in
one file — `Player` lines fan out across the player voice slots as usual:

```
u1: Narration read by the default voice.
u2 | Toppo: Welcome back, cadet.
u3 | Player: On my way.
```

```bash
mhsaudio generate -in toppo-lessons.txt -voices voices.json \
  -out ./lessons -default-speaker Toppo -timestamps
```

Emotion on v3 voices (the dry run's sample and per-voice model column show
what each file renders with):

```bash
mhsaudio generate -in export.csv -voices voices.json -out ./audio -emotion -dry-run
mhsaudio generate -in export.csv -voices voices.json -out ./audio -emotion
```

### `generate` flags

| Flag | Meaning | Default |
|---|---|---|
| `-in` | dialog source file | *(required)* |
| `-voices` | `voices.json` or a `VoiceAssignments.csv` | *(required)* |
| `-out` | output folder | *(required)* |
| `-source` | `auto`, `dbexport`, or `simplescript` | `auto` |
| `-layout` | `dialog-system` or `babylon-manifest` | `dialog-system` |
| `-format` | ElevenLabs `output_format` | `mp3_44100_128` |
| `-timestamps` | also write `<id>.words.json` word timings | off |
| `-concurrency` | parallel requests (0 = account max) | `0` |
| `-force` | regenerate everything | off |
| `-prune` | after a clean run, delete files for removed lines | off |
| `-delta` | copy this run's written files into a folder | — |
| `-no-cleanup` | disable text cleanup | off (cleanup on) |
| `-profile` | custom cleanup profile JSON | built-in `mhs-dialogue` |
| `-model` | model for voices that don't set one: `v2`, `v3`, or a full ID | `v2` |
| `-emotion` | directions → v3 audio tags; stripped from v2 voices' text | off |
| `-emotion-map` | custom emotion map JSON (implies `-emotion`) | built-in `mhs-emotion` |
| `-voice-overrides` | per-line delivery tweaks JSON (see below) | — |
| `-pronunciations` | pronunciations JSON (server-side dictionary) | shared per-user file |
| `-no-pronunciations` | disable the pronunciation dictionary | off (dictionary on) |
| `-default-speaker` | character voice for speaker-less lines | — |
| `-default-voice` | raw voice ID for speaker-less lines (alternative to `-default-speaker`) | — |
| `-key-file` | file holding the ElevenLabs API key | env, then `~/.elevenlabs_key` |
| `-dry-run` | preview only, no API calls | off |
| `-v` | list every file | off |

### Resuming

Re-running the same command only generates new or changed lines (it reads the
manifest in the output folder). Press **Ctrl-C** to stop; the next run picks up
where it left off. Use `-force` to redo everything.

One migration note: output folders generated before the manifest recorded
voice/model/format are honored as-is (nothing regenerates on upgrade) — but
that also means the tool can't detect a voice recast that happened *before*
the upgrade. If casting changed since those files were made, run once with
`-force`.

### Voice settings & per-line tweaks

Each voice can carry its own delivery settings — **stability**, **style**,
**similarity**, **speaker boost**, **speed** — set in the app's voice editor
(the ⚙ button on a row; the ▶ preview auditions them). Blank knobs stay at the
voice's ElevenLabs defaults; settings are saved in `voices.json` and survive
CSV re-imports.

For single lines that need something special, a `voice-overrides.json` (kept
next to `voices.json`, owned by whoever directs the audio) maps line IDs to
tweaks of the audible knobs:

```json
{
  "U1_Toppo_2":  { "stability": 0.3, "speed": 1.1 },
  "U2_DANI_14":  { "style": 0.6 }
}
```

App: **Line tweaks → Choose voice-overrides.json…** in the options. CLI:
`-voice-overrides voice-overrides.json`. A player line's tweak applies to every
slot's rendering. Changing any setting regenerates exactly the affected files
on the next run — no `-force` needed.

### Pronunciation

Words like `WAT247` are pronounced correctly by a server-side ElevenLabs
**pronunciation dictionary** instead of rewriting the text: the request keeps
the writers' spelling, so captions and word timings align to the displayed
text — `WAT247` is one timed token spanning the whole spoken phrase, which is
what caption highlighting needs. (Verified live; see
`docs/pronunciation-dictionaries.md`.)

Rules live in a `pronunciations.json` — by default a shared per-user file
seeded with the MHS rules; the app's **Edit rules…** editor changes them (CLI:
`-pronunciations file.json` for a project-specific file, `-no-pronunciations`
to disable). When rules change, the dictionary republishes to the account
automatically at the start of the next run, and only the lines containing a
changed word regenerate.

Note: pronunciation used to be part of the cleanup profile as text
replacement. Old custom cleanup profiles that still carry those rules keep
working, but they rewrite the text (captions show the respelling) — remove
them from custom profiles to get the dictionary behavior.

### API key storage

The key resolves in this order: `$ELEVENLABS_API_KEY` → `-key-file` → the
**macOS Keychain** → `~/.elevenlabs_key`. On macOS you can keep it encrypted in
the Keychain instead of the dotfile — entirely optional, nothing migrates by
itself:

- **App**: the key screen has a "Store in the macOS Keychain" checkbox
  (checked by default on Macs).
- **CLI**: `mhsaudio key` shows where the key currently comes from (never the
  key itself); `mhsaudio key -store-keychain` copies it into the Keychain, and
  adding `-rm-file` also deletes the plaintext dotfile.

Windows/Linux keep using the dotfile (or the env var).

### Updating the game project (Unity / Perforce)

**Step-by-step walkthrough with both scenarios:
[docs/updating-dialog-audio.md](updating-dialog-audio.md).** The short version:

Keep **one** canonical output folder and always generate into it — the manifest
makes re-runs incremental, so a new dialog export only synthesizes changed and
added lines (no manual CSV diffing needed). Then move just the differences into
the game project:

- **Changed/added files** — after a run, the app's result card offers
  **"Copy N changed file(s)…"**: it copies exactly what that run wrote (audio,
  timing sidecars, and the Babylon manifest when produced) into a folder you
  pick, preserving the folder layout. Drop that folder onto the Unity project
  so only those files import. CLI: `-delta <folder>` does the same.
- **Removed lines** — the Preview shows any **orphaned files** (audio for lines
  no longer in the source) with a **"Remove orphaned files"** button; the CLI
  lists them and `-prune` deletes them after a fully successful run. Pruning
  only ever touches files this tool created (it works from the manifest) and
  refuses to run if the plan has problems, so a broken voices file can't make
  good audio look orphaned. Deleting those files from the game project /
  Perforce remains a manual step — the pruned list tells you which ones.

### Job history

Both the app and the CLI record every run in a shared history (in your user
config folder, capped at 50 entries). The app shows it as a **Recent jobs**
card — an interrupted run (app quit or crash mid-batch) is flagged there with a
**Resume** button, and any job can be re-run, opened, or removed. From the
terminal, `mhsaudio jobs` lists the same history.

### Babylon manifest

With `-layout babylon-manifest`, each completed run also writes
`ceremony_audio.json` into the output folder — the manifest the ceremony player
loads, listing every file with its `assets/audio/…` URL, word-timing caption
pairs, duration, and text hash. Generate with `-timestamps` so the caption data
is populated.

### Exit codes

`0` only when everything asked for succeeded. A run where any file failed, or a
dry run that found problems, exits non-zero — so scripts can chain on it
(`mhsaudio generate … && deploy`).
