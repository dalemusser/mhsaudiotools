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
   The panel shows each character's voice and the player slots; warnings appear if
   something's missing.
4. **③ Output** — choose the folder for the audio. Options:
   - **Layout** — leave on *Dialogue System (game)*.
   - **Audio** — MP3 quality.
   - **Default voice** — the character whose voice reads a writer script's lines
     (e.g. `Toppo`); leave blank for Dialogue System exports.
   - **Parallel** — how many lines to generate at once. Defaults to your account's
     maximum, which is fastest. Lower it if a teammate is generating at the same
     time on the same account.
   - **Clean up text** (on), **Word timings** (off), **Regenerate everything** (off).
5. **Preview** — shows what it *would* do (lines, files, characters, files-per-voice)
   without calling ElevenLabs or spending anything. Check the per-voice counts look
   right.
6. **Generate** — runs it, with a progress bar. **Stop** cancels; what's done is
   kept, and running again resumes.
7. **Open output folder** — the audio is ready to drop into the game.

---

## The CLI

```
aigenaudio <command> [flags]

  account         show the account tier and its max parallelism
  voices          list the voices available on the account
  import-voices   convert/merge a VoiceAssignments.csv into a voices.json
  generate        generate audio (use -dry-run to preview)
```

The API key comes from `$ELEVENLABS_API_KEY`, `-key-file <path>`, or `~/.elevenlabs_key`.

### Set up voices once

```bash
# Convert the team's CSV to a voices.json (preserves player-slot bindings on re-import)
aigenaudio import-voices -csv VoiceAssignments.csv -out voices.json

# Browse available voices to fill in / correct assignments
aigenaudio voices -filter toppo
```

### Preview, then generate

```bash
# Dry run — no API calls, nothing written. Always do this first on a new export.
aigenaudio generate -in 2026-06-10-MHSDialogueExport.csv \
  -voices voices.json -out ./audio -dry-run

# Real run. Parallelism auto-detects the account's max; add -v for per-file logs.
aigenaudio generate -in 2026-06-10-MHSDialogueExport.csv \
  -voices voices.json -out ./audio
```

A writer script needs a default voice:

```bash
aigenaudio generate -in toppo-lessons.txt -voices voices.json \
  -out ./lessons -default-speaker Toppo -timestamps
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
| `-default-speaker` | character voice for speaker-less lines | — |
| `-dry-run` | preview only, no API calls | off |
| `-v` | list every file | off |

### Resuming

Re-running the same command only generates new or changed lines (it reads the
manifest in the output folder). Press **Ctrl-C** to stop; the next run picks up
where it left off. Use `-force` to redo everything.
