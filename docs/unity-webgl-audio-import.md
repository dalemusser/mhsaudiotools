# Unity audio import settings for MHS dialog (WebGL)

*Prepared 2026-07-28 for the import-settings discussion. Supersedes the two
earlier notes (`unity-import-settings.md`, `unity-import-settings-2.md`) —
this version is checked against Unity's current Web-platform documentation and
sized for our actual situation. Where it contradicts the earlier notes, this
one wins; the biggest reversal is the Load Type recommendation, explained
below.*

## Our situation

- **~6,000 MP3 files** from ElevenLabs, one file per dialog line, a few
  seconds each (~6–7 hours of audio total).
- The game currently ships as **5 separate builds, one per unit**, each
  containing only its own unit's audio (roughly a fifth of the library,
  ~30–50 MB compressed).
- **Pixel Crushers Dialogue System**: the line's entry ID is the filename; the
  Dialogue System finds and plays the clip for the line being shown (e.g. the
  `AudioWait(entrytag)` sequencer command).
- **WebGL build**, with players substantially on **Chromebooks** — so both the
  initial download size and browser memory matter more than usual.

## What's different about audio on the Web platform

These are the facts the recommendation rests on (Unity Manual, "Audio in Web"
and "AudioClip import settings", checked July 2026):

1. **Every clip is re-encoded to AAC in the build**, regardless of source
   format or the compression dropdown you see. Two consequences:
   - **Never convert the ElevenLabs MP3s to WAV first.** The MP3's lost
     information cannot come back; you'd only inflate the project. Import the
     MP3s as-is: MP3 → Unity decodes → AAC in the build, and a WAV detour adds
     nothing but disk usage.
   - The Vorbis/PCM/ADPCM discussion from the desktop world mostly doesn't
     apply; the **Quality slider still matters** because it governs the
     encode's bitrate (size vs. fidelity).
2. **Unity's audio engine (FMOD) isn't used on Web** — playback goes through
   the browser's Web Audio API. Compressed clips are decoded by the browser
   (`decodeAudioData`), which produces **float32 PCM buffers** and may resample
   to the browser's rate (usually 48 kHz) no matter what the import settings
   say.
3. **Only two Load Types work on Web**: Decompress On Load and Compressed In
   Memory. **Streaming is not supported** in Web builds — ignore any desktop
   advice built around it.
4. Unity's own Web guidance **reverses the usual dialog advice**: on Web,
   *Decompress On Load* is recommended for dialog and effects (low latency,
   precise start times), while *Compressed In Memory* is for long music
   (it trades latency and playback precision for memory). The earlier note's
   "Compressed In Memory for dialogue" was desktop guidance; on Web, with
   on-demand loading, it's the wrong default — the memory problem is solved by
   loading fewer clips at a time, not by keeping thousands resident in
   compressed form. Unity also notes Chromium-based browsers may effectively
   force decompress-on-load behavior anyway.

### The memory and size math

Decoded audio is big: our full ~6–7 hours as float32 at 48 kHz mono would be
roughly **4–5 GB** if everything were decoded at once — instant death on a
Chromebook. But that only happens if all 6,000 clips are *loaded* at startup.
One 4-second line decoded is under **1 MB**, and dialog plays one line at a
time. So the strategy is not "keep clips compressed in memory" — it's **"only
load the clip(s) actually being used, and let each decode on load."** That's
what the Preload and packaging choices below arrange.

The *compressed* side is the second problem at this scale: 6,000 clips at
mono, optimized sample rate, mid quality come to roughly **150–250 MB of AAC**.
That is too much to ship inside the initial WebGL download — which is why
packaging (next section) matters as much as the per-clip settings.

## How to apply settings (the mechanics)

1. Select all the dialog clips in the Project window (they can be multi-selected
   or you select the folder's contents) → the Inspector shows the shared
   AudioClip import settings → change them once → **Apply**. Unity reimports.
2. Load Type / Compression / Quality / Sample Rate / Preload live under the
   **platform tabs** ("Default" plus one per build target). Set the Default
   tab, and use the **WebGL tab with "Override" ticked** for anything
   Web-specific.
3. To make the settings stick for **future imports** (each new delta of
   generated files): create a **Preset** from one correctly-configured clip
   (the slider-row ⋮ → Save Preset), then in *Project Settings → Preset
   Manager* register it as the default AudioImporter preset — with a filter so
   it applies only to the dialog audio folder. New/updated MP3s dropped in
   then import with the right settings automatically, which matters for our
   changed-files-only update flow.

## The settings, option by option

| Setting | Options | Trade-offs |
|---|---|---|
| **Force To Mono** | on/off | ElevenLabs speech is effectively mono; stereo stores two near-identical channels. On = ~half the size and half the decoded memory. No downside for voice. |
| **Normalize** | on/off (with mono mixdown) | Changes loudness during mixdown. Leave **off** — level consistency should come from generation/mixer, not per-clip normalization that could make lines jump in volume. |
| **Load In Background** | on/off | Loads clip data asynchronously instead of blocking. On = no hitch when a clip loads mid-scene; the only cost is the clip isn't instantly ready the same frame — the Dialogue System's audio commands wait, so this is free insurance. |
| **Load Type** (Web) | Decompress On Load / Compressed In Memory | **Decompress On Load**: browser decodes when the clip loads; playback is immediate and sample-accurate. Memory cost is per *loaded* clip (small, if loading is on-demand). **Compressed In Memory**: smaller resident memory per loaded clip but added latency and less precise starts — Unity recommends it for music, not dialog; and Chromium may decompress anyway. **Streaming**: unavailable on Web. |
| **Compression Format** | (becomes AAC on Web) | The dropdown matters little on Web — the build encodes AAC either way. Keep the default compressed option; PCM/ADPCM would only bloat other platforms if you ever add them. |
| **Quality** | 1–100 slider | Bitrate vs. size of the encoded audio. Speech is forgiving: 40–55 is usually indistinguishable from 100 for game dialog; below ~35 artifacts creep in. Directly scales the ~15–25 MB build cost. |
| **Sample Rate Setting** | Preserve / Optimize / Override | Voice has limited high-frequency content; **Optimize** lets Unity drop e.g. 44.1 kHz → ~22–24 kHz per clip when inaudible, shrinking the build. (The browser resamples at decode regardless, so Preserve buys nothing audible.) |
| **Preload Audio Data** | on/off (per platform) | On = every clip referenced by loaded assets decodes at scene load — with Decompress On Load across hundreds of clips, that's the 0.4 GB trap. **Off** = a clip's audio loads on first use. Off is essential here. |

## Recommendation for MHS

| Setting | Value | Why |
|---|---|---|
| Source files | ElevenLabs MP3s, unchanged | WAV conversion adds size, not quality |
| Force To Mono | **On** | Half size, half memory, no audible cost for voice |
| Normalize | Off | Keep authored levels |
| Load In Background | On | Hide load hitches; audio commands wait anyway |
| Load Type (WebGL tab) | **Decompress On Load** | Unity's Web guidance for dialog: instant, precise starts; per-clip decode is <1 MB |
| Preload Audio Data | **Off** | The pairing that makes Decompress On Load safe: decode per use, not 6,000 at startup |
| Quality | **50** to start | A/B test 35 / 50 / 65 on real lines; ship the lowest that passes on laptop/Chromebook speakers |
| Sample Rate | Optimize | Free size win for speech |
| Packaging | **Addressables, grouped finer than the unit** (conversation/scene grain) — or keep build-resident if whole-unit-forever is the only option the team would use | With 5 per-unit builds, audio is ~30–50 MB per build; whole-unit bundles are memory-neutral, so the case rests on grain, startup, and rebuild-free updates — see "Honest sizing" |

## Packaging: the decision that matters most at 6,000 clips

A `Resources` folder is the convenient path for small libraries, but everything
in `Resources` is baked into the main build data, which the browser downloads
**before the game starts**. At ~150–250 MB of dialog audio, that alone could
multiply the initial download and add thousands of asset records to startup.
Even with Preload off, the *compressed* data still travels inside the initial
package — Preload only controls decoding, not downloading.

So at this scale, split the dialog out of the main build:

- **Group clips by how the game consumes them** — per unit/chapter, per
  location, or per conversation cluster. The natural grain for MHS is probably
  the curriculum unit.
- **Package the groups as Addressables** (or classic AssetBundles — Pixel
  Crushers supports entrytag lookup against either, as well as Resources).
  Load a group shortly before it's needed — entering a unit/scene — and
  release it afterwards. Peak memory then holds one group's compressed data,
  not the whole library, and decoded buffers only for lines in flight.
- **Remote vs. local groups:** remote Addressable groups (fetched from a
  server/CDN on demand) keep the initial download small and let dialog updates
  ship without rebuilding the player — and they dovetail with the Chromebook
  PWA delivery design (pre-cache/background-fetch could warm exactly these
  bundles). Local (shipped-alongside) groups still cut startup memory and
  asset-record cost, just not the download.
- **Update flow still works:** our generator's changed-files-only export tells
  you which clips changed; only the groups containing those clips need
  rebuilding/re-uploading.

The per-clip import settings above are unchanged by any of this — they apply
identically whether a clip lives in Resources or a bundle.

### Where the bytes actually live (why this works)

On WebGL the build's "virtual file system" is not a disk — **it's RAM**.
Everything in the main `.data` file is downloaded and stays resident in
browser memory for the whole session. Audio shipped in the build therefore
costs its full compressed size (~150–250 MB here) in memory permanently,
used or not, on top of the transient decoded buffer whenever a line plays.
An Addressables bundle's compressed bytes also live in memory — but only
**while the group is loaded**, and releasing the group genuinely returns the
memory; re-entering a unit re-loads the bundle from the browser/IndexedDB
cache without re-downloading. A playing clip briefly exists compressed +
decoded under either approach; the win is scope and lifetime of the
compressed copy, not the decode path.

**Honest sizing for our 5-build structure:** the per-unit builds already scope
the resident audio to one unit (~30–50 MB). A single whole-unit addressable
loaded at unit start and held all session is therefore **memory-neutral** —
bundle heap instead of VFS RAM, same magnitude. In our structure the real
Addressables benefits are: **(1) finer grain** — group/label at
conversation-or-scene level and residency drops to the current section's
clips, which build-resident audio can never do; **(2) startup** — the unit is
interactive without waiting on its audio payload, which downloads behind the
scenes (or is PWA-pre-cached); **(3) audio updates without rebuilding the
unit's build** — upload changed bundle + catalog, paired with the generator's
changed-files export; **(4) the path to a single build** loading unit content
on demand. If the team would only ever do whole-unit bundles held forever,
benefits reduce to (2) and (3) — decide accordingly.

*Implementation how-to:
[unity-addressables-dialog-audio.md](unity-addressables-dialog-audio.md).*

### Addressables vs. raw AssetBundles

**Runtime performance is a wash: Addressables *is* AssetBundles underneath.**
It builds bundles, ships bundles, and loads bundles (via UnityWebRequest on
Web); download size, decode cost, and memory are the same when configured
equivalently. Addressables adds only a small content catalog fetched at
startup and some ref-counting bookkeeping — noise next to the audio payload.
The Dialogue System is neutral too: `AudioWait(entrytag)` resolves clips from
Resources, registered AssetBundles, or Addressables (key = entrytag) alike.

The difference is who does the management work:

| Concern | Raw AssetBundles | Addressables |
|---|---|---|
| Assigning clips to bundles | By hand / your build script | Groups window; drag folders |
| Building | Your `BuildPipeline` script | Built-in build (incremental) |
| Knowing what's in which bundle | Your own bookkeeping | Content catalog, automatic |
| Load/unload | Manual `Unload(true/false)` — classic footguns | Ref-counted `Release` |
| Versioning & browser caching | Hand-rolled (hashes in names, Caching API) | Catalog + cache handled |
| **Updating dialog without rebuilding the player** | Build your own update scheme | Built-in content-update workflow |
| Cost | No package dep; total control | Extra package, learning curve, more abstraction when debugging |

For MHS the update column is the one that matters: dialog changes keep coming,
and the generator's changed-files export maps exactly onto "rebuild and
re-upload only the groups containing changed clips" — with Addressables the
updated catalog does the version juggling and clients pick up new audio
without a new player build. **Recommend Addressables** unless the project
already has a working AssetBundle pipeline the developer wants to keep — in
which case staying is defensible, and performance won't be the deciding
factor either way.

## To verify in a real Web build (not the Editor)

1. **Initial download size** with and without the dialog audio in the main
   build — this is the number that justifies (or not) the Addressables work.
2. **Group load/unload behavior**: enter a unit, play dialog, leave — confirm
   the group's memory is actually released.
3. **First-line latency**: start a conversation whose clip wasn't loaded yet —
   does audio start with the text, or lag it? (If it lags: warm the group
   shortly before the conversation, not the whole library at startup.)
4. **Rapid advancing/skipping** through lines — `AudioWait()` should stop the
   old line cleanly; watch for decode churn.
5. **Memory after several long conversations** (browser task manager on an
   actual Chromebook): confirm clips are being released, not accumulating.
   With build-resident audio, Resources-loaded clips linger until
   `Resources.UnloadUnusedAssets()` runs (usually a scene change) — if memory
   stair-steps here, add a conversation-end unload hook; addressable loads
   are released per line automatically. Details and the hook script:
   [unload-dialog-clips.md](unload-dialog-clips.md).
6. **The quality A/B**: three builds at 35/50/65 — at ~150–250 MB total,
   quality 40 vs 60 swings the audio payload by tens of MB; compare size and
   clarity on the worst speakers anyone will use.
7. **`AudioClip.frequency` at runtime** if any code depends on it — the browser
   may report the context rate, not the imported rate.

## Questions for the meeting

- Which Unity version is the project on? (This doc reflects current Unity
  docs; older versions differ in Web audio details.)
- How exactly are the clips resolved today — a `Resources` folder with
  `AudioWait(entrytag)`? Direct references? That determines whether the
  Preload/on-demand behavior above applies as described.
- Are any dialog clips *scene-referenced* (which would pull them into scene
  load) rather than looked up by entrytag?
- Is there an existing memory budget/target for the Chromebook builds this
  should fit inside — and a target for the initial download size?
- What's the natural grouping for the dialog — curriculum unit, scene,
  conversation? (It sets the Addressables group boundaries and how much audio
  is resident at once.)
- If groups go remote: where would the bundles be hosted, and should the
  Chromebook PWA pre-cache plan cover them so classrooms aren't blocked on
  mid-session downloads?
