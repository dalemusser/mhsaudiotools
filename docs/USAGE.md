# Using the tools

Both tools do the same thing: take **dialog**, apply your **voices**, and write
**audio files** to a folder. This guide covers the app first, then the CLI.

## Concepts (read once)

- **Dialog source** — where the lines come from. Two formats are supported:
  - **Dialogue System export** (`.csv`) — the game's dialog database export. The
    line's ID becomes the audio filename; the speaker is read from the ID.
  - **Writer script** (`.txt`) — one line per row as `id: spoken text`. Use it for
    a writer's cutscene or lesson script. These lines have no speaker, so you set a
    **default voice**.
- **Voices** — which ElevenLabs voice speaks each character, kept in a
  `voices.json`. You can import the team's `VoiceAssignments.csv` and the tool
  converts it. The player character has **numbered voice slots** (Player1…Player6);
  each slot stays bound to the same voice across regenerations, so re-running never
  moves a voice to a different `PlayerN` folder.
- **Output layout** — *Dialogue System* (the default) writes NPC files flat at the
  top level and player lines into `Player1/`…`PlayerN/`, exactly as the game loads
  them. *Babylon manifest* is for the web projects.
- **Cleanup** — strips stage directions and formatting (`[em1]`, `{{PLACEHOLDER…}}`)
  and fixes pronunciations (`WAT247` → "Watt 2 4 7") before speaking. On by default.
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

Cleanup (removing stage directions/markup, fixing pronunciations) uses a profile.
By default it's the built-in `mhs-dialogue` rules; keep **one shared `cleanup.json`**
the team edits over time. Create it from the defaults (in the app: *Save MHS
defaults…*), then edit it as new markup or pronunciation issues turn up.

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

A writer script needs a default voice:

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
| `-no-cleanup` | disable text cleanup | off (cleanup on) |
| `-profile` | custom cleanup profile JSON | built-in `mhs-dialogue` |
| `-model` | model for voices that don't set one: `v2`, `v3`, or a full ID | `v2` |
| `-emotion` | directions → v3 audio tags; stripped from v2 voices' text | off |
| `-emotion-map` | custom emotion map JSON (implies `-emotion`) | built-in `mhs-emotion` |
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

### Exit codes

`0` only when everything asked for succeeded. A run where any file failed, or a
dry run that found problems, exits non-zero — so scripts can chain on it
(`mhsaudio generate … && deploy`).
