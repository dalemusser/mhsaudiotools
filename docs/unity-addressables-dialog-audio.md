# Implementing Addressables for the dialog audio

*Companion to [unity-webgl-audio-import.md](unity-webgl-audio-import.md), which
covers the per-clip import settings and why packaging matters at ~6,000 clips.
This document is the how-to for the packaging side: moving the dialog audio
out of the initial WebGL download into Addressables, grouped by unit, working
with the Pixel Crushers Dialogue System's entrytag lookup.*

## How the pieces fit

- Our generator names every file by its **entrytag** (`8_Toppo_2.mp3`), the
  same ID the Dialogue System uses to find a line's audio.
- The Dialogue System's audio sequencer commands (`AudioWait(entrytag)` etc.)
  look for a clip by that name in **Resources folders, AssetBundles, and
  Addressables** — so switching to Addressables changes *where clips live*,
  not how conversations reference them. No dialog database changes.
- With Addressables, the Dialogue System **loads the clip per line and
  releases it when the sequencer command ends** — so per-line memory
  management comes for free; our job is grouping, delivery, and updates.

## One-time setup

### 1. Install and initialize Addressables

1. Package Manager → install **Addressables** (`com.unity.addressables`).
2. *Window → Asset Management → Addressables → Groups* → **Create
   Addressables Settings**. This creates `AddressableAssetsData/` (commit it
   to Perforce — it's part of the project).

### 2. Enable the Dialogue System's Addressables support

In the Dialogue System's **Welcome Window**, enable the Addressables checkbox
(it adds the `USE_ADDRESSABLES` scripting define). Without this, the sequencer
won't search Addressables at all — the classic "works in Resources, silent in
Addressables" mistake.

### 3. Organize the audio folders by group

Arrange the imported audio in the Project by the grain you'll load at — the
curriculum unit is the natural choice:

```
Assets/DialogAudio/
  Unit1/   (clips + PlayerN/ subfolders, exactly as the generator lays them out)
  Unit2/
  …
```

**Move the clips out of any `Resources` folder.** If a clip exists in both
Resources and Addressables it ships twice, bloating the build the whole point
is to shrink — and lookups become ambiguous about which copy they find.

### 4. Create the groups and addresses

1. In the Groups window, create one group per unit (`DialogAudio-Unit1`, …)
   and drag each unit's folder in.
2. **Addresses must equal entrytags.** Select the entries → right-click →
   **Simplify Addressable Names** — this sets each address to the filename
   without extension, which *is* the entrytag. That single step is what makes
   `AudioWait(entrytag)` resolve.
3. **⚠ Player-slot clips need a decision.** Player lines exist once per voice
   slot (`Player1/8_Player_12.mp3`, `Player2/8_Player_12.mp3` …) — same
   filename, so *simplified* addresses would collide, and addresses must be
   unique. Options:
   - address them slot-qualified (`Player2/8_Player_12`) and have the
     sequence/subtitle code prepend the chosen slot (mirroring however the
     game selects the PlayerN folder today), or
   - only mark the *selected* slot's folder addressable per build/profile
     (unlikely to fit a runtime voice choice).
   How the game currently picks the player folder determines the right scheme
   — resolve this with the Unity developer before bulk-assigning addresses.
4. Add a **label** per unit (`unit1`, `unit2`, …) to the group's entries —
   labels are how you pre-download a whole unit in one call.

### 5. Group settings (per group, Inspector)

- **Bundle Mode: Pack Together** — one bundle per unit (~15–40 MB each).
  *Pack Separately* would make 6,000 tiny bundles (catalog bloat, request
  storms); one-per-unit is the right starting grain. If a unit's bundle gets
  unwieldy, split that group by conversation cluster later.
- **Compression: LZ4** (default) is fine — the audio inside is already AAC;
  bundle compression adds little either way.
- Build & Load Paths: start with **Local** (bundles ship next to the WebGL
  build, served from the same host). This already achieves the *memory* win —
  only loaded groups occupy browser memory. Move groups to **Remote** when you
  want the initial-download win and rebuild-free updates:
  - *Window → Asset Management → Addressables → Profiles*: set
    `Remote.LoadPath` to the hosting URL.
  - In AddressableAssetSettings, enable **Build Remote Catalog** — the catalog
    is what lets deployed players discover updated audio.

## Build and deploy

1. Addressables are **built separately from the player**: Groups window →
   **Build → New Build → Default Build Script**. Do this before each player
   build (or wire it into the build script — there's an API for CI).
2. Local groups end up inside the build output automatically. Remote groups
   land in `ServerData/WebGL/` — upload that folder (bundles + catalog
   `.json`/`.hash`) to the hosting URL.
3. WebGL loads bundles over HTTP(S) — the host must allow **CORS** for the
   game's origin, and serve the files with sane cache headers (the catalog
   hash file is how clients detect updates, so don't cache-forever the
   catalog).

## Runtime behavior and warm-up

- Per line, nothing to do: the sequencer loads the addressable by entrytag,
  plays, releases. Memory holds the *current group's bundle* (compressed) plus
  the decoded clip(s) in flight.
- To hide download latency on remote groups, **pre-download a unit when the
  player enters it**:

  ```csharp
  // fire-and-forget warm-up; bundles land in the browser's cache
  Addressables.DownloadDependenciesAsync("unit2", autoReleaseHandle: true);
  ```

  This is also the hook the Chromebook PWA pre-cache design can use — the
  bundle URLs are stable between content updates, so a service worker or
  classroom pre-cache step can fetch them ahead of the session.

## Updating dialog audio (ties into our generator)

After a dialog update, the generator's **changed-files export** says exactly
which clips changed — and therefore which unit groups are affected.

- **Simplest workflow (recommended):** replace the changed MP3s in the project
  (the changed-files staging folder drops straight onto `Assets/DialogAudio/`),
  rebuild Addressables, upload the output. Bundle files are content-hashed, so
  only the bundles that actually changed produce new files to upload; the new
  catalog points players at them. **No player rebuild** for remote groups.
- New lines: the new files inherit addressability from the folder/group; run
  **Simplify Addressable Names** on the new entries (or automate address
  assignment with an `AssetPostprocessor` so imports self-address).
- Removed lines: the generator's prune list says which clips to delete from
  the project (and Perforce) before rebuilding.

## Pitfalls checklist

- [ ] Dialogue System's Addressables support actually enabled (Welcome
      Window) — silent failure otherwise.
- [ ] Clips removed from `Resources` after becoming addressable.
- [ ] Addresses simplified to bare entrytags; **player-slot collision scheme
      decided** before bulk assignment.
- [ ] Addressables rebuilt before every player build (stale catalogs point at
      bundles that no longer exist).
- [ ] Remote host: CORS headers, catalog not cached-forever.
- [ ] Test in a real WebGL build, not the Editor — Editor play mode can fake
      asset loading ("Use Asset Database" play mode script) and hide all of
      the above.
- [ ] First line of a fresh unit: verify audio starts with the text after a
      cold load; add the warm-up call where it doesn't.

## References

- Dialogue System: entrytags & sequencer audio —
  pixelcrushers.com/dialogue_system/manual2x/html/cutscene_sequences.html
- Dialogue System: sequencer command reference (`AudioWait`) —
  pixelcrushers.com/dialogue_system/manual2x/html/sequencer_command_reference.html
- Unity Addressables manual — docs.unity3d.com/Manual/com.unity.addressables.html
