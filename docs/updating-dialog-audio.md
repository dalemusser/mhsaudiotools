# Updating dialog audio when the script changes

The scenario this guide covers: you generated audio from a dialog export, the
writers changed lines and added new ones (and maybe cut some), and now you have
a **new export CSV**. You want the new audio — and you want to update the Unity
project without re-importing thousands of unchanged files.

**You do not need to diff the CSVs anymore.** The old workflow — diff the prior
export against the new one, generate just the changed lines into a separate
folder — is what the tool now does for you. Every output folder carries a
manifest recording what each audio file was generated from (text, voice, model,
format, settings, pronunciation rules). A run compares the new export against
that manifest per line and:

- **skips** unchanged lines (no API call, no cost),
- **regenerates** lines whose text — or voice, model, settings, pronunciation —
  changed, overwriting the same file,
- **generates** new lines,
- **reports** files whose lines were removed (they're never deleted silently).

Line identity is the dialog ID (the entrytag, which is also the filename), so
this works across any number of exports as long as IDs stay stable — which the
Dialogue System guarantees.

## The one rule that makes it work

**Keep one canonical output folder and always generate into it.** The folder's
manifest is the memory; a fresh folder has no memory and regenerates
everything. The export CSV can live anywhere and be named anything — only the
output folder matters.

---

## Scenario 1 — an update: import just the changes

The common case: a new export lands, you want the new/changed audio in Unity
with minimal import and Perforce churn.

### In the app

1. **Choose the new export** as the dialog source; keep the same voices file
   and the **same output folder** as always.
2. Click **Preview**. Before anything is paid for, it shows the diff the
   manifest computed: *to generate N, already up to date M*, the files per
   voice, sample lines — and, if lines were cut, an amber box listing the
   **orphaned files** with a "Remove orphaned files" button.
3. Click **Generate**. Only the N changed/added files are synthesized.
4. On the result card, click **"Copy N changed file(s)…"** and pick an empty
   staging folder. Exactly what this run wrote — audio, any `.words.json`
   timing sidecars, the Babylon manifest if that layout is used — is copied
   there with the folder layout preserved (`Player1/…` etc.).
5. **Unity/Perforce:** copy the staging folder's contents onto the audio
   location in the Unity project. Only those files import; Perforce sees only
   those adds/edits.
6. If the Preview showed orphans, click **Remove orphaned files** — the tool
   deletes them from the output folder and shows the list. **Delete those same
   files from the Unity project / Perforce by hand** (the tool can't reach
   into the game project); the list is your checklist.

### From the terminal

```bash
# 1. Preview the diff — free, no API key needed:
mhsaudio generate -in NewExport.csv -voices voices.json -out ./audio -dry-run

# 2. Generate only the changes, stage them for import, clean up removed lines:
mhsaudio generate -in NewExport.csv -voices voices.json -out ./audio \
  -delta ./to-import -prune
```

What that prints (abridged):

```
Source:   NewExport.csv (dbexport) — 5,912 lines
…
Orphans:  2 file(s) from removed lines — will be pruned
…
  1,342/1,342 files (100%)  failed 0

Done in 4m12s
  files written:  1,342
  …
Pruned 2 orphaned file(s):
  8_Toppo_77.mp3
  Player2/8_Player_140.mp3
(delete these from the game project / Perforce too)

Delta: 1,342 changed file(s) copied to ./to-import
```

Then copy `./to-import` onto the Unity project. `-prune` only acts when the run
finished fully clean; the orphan list is what you remove from Perforce.

---

## Scenario 2 — refresh the whole collection

Sometimes you want the complete, current set rather than a delta: the first
time audio goes into the project, after a wide-reaching change (recasting many
voices, a settings overhaul) where reviewing the whole set makes sense, or when
you don't trust that the project's copy matches.

1. Generate into the same canonical folder as always. You usually do **not**
   need "Regenerate everything"/-force — voice, model, settings, and
   pronunciation changes already regenerate exactly the affected files. Force
   is only for fresh takes of unchanged lines or files modified outside the
   tool (see USAGE, "Regenerate everything").
2. Prune orphans (app button or `-prune`) so the folder contains no dead files.
3. Import the **entire output folder** into the Unity project, replacing the
   audio directory wholesale. Perforce reconciles what actually differs; import
   time is the full-collection cost — that's the tradeoff of this scenario.
4. Skip the delta/staging step entirely — the canonical folder *is* the
   deliverable here. (Leave `.mhsaudio-manifest.json` out of the Unity copy;
   it's the tool's bookkeeping, not game data.)

---

## Reference: what goes where

| | Scenario 1 (changes only) | Scenario 2 (whole collection) |
|---|---|---|
| Generate into | the canonical output folder | the canonical output folder |
| Import into Unity from | the staging folder (`-delta` / "Copy changed files…") | the whole output folder |
| Perforce adds/edits | just the staged files | whatever reconcile finds |
| Perforce deletes | the pruned-orphans list | the pruned-orphans list |
| Import time | minutes | the full set |

Both scenarios generate identically — the only difference is what you carry
into the game project afterwards. The canonical folder plus its manifest stays
the single source of truth either way, and the next update starts from it.
