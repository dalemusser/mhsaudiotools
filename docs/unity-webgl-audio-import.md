# Unity audio import settings for MHS dialog (WebGL)

*Prepared 2026-07-28 for the import-settings discussion. Supersedes the two
earlier notes (`unity-import-settings.md`, `unity-import-settings-2.md`) —
this version is checked against Unity's current Web-platform documentation and
sized for our actual situation. Where it contradicts the earlier notes, this
one wins; the biggest reversal is the Load Type recommendation, explained
below.*

## Our situation

- **~600 MP3 files** from ElevenLabs, one file per dialog line, a few seconds
  each (~40 minutes of audio total).
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
   "Compressed In Memory for dialogue" was desktop guidance scaled for a
   hypothetical 5,000 clips; at our 600, on Web, it's the wrong default.
   Unity also notes Chromium-based browsers may effectively force
   decompress-on-load behavior anyway.

### The memory math that makes this safe

Decoded audio is big: our full ~40 minutes as float32 at 48 kHz mono would be
roughly **0.4–0.5 GB** if everything were decoded at once — a Chromebook
killer. But that only happens if all 600 clips are *loaded* at startup. One
4-second line decoded is under **1 MB**, and dialog plays one line at a time.
So the strategy is not "keep clips compressed in memory" — it's **"only load
the clip(s) actually being used, and let each decode on load."** That's what
the Preload and packaging choices below arrange. Meanwhile the *compressed*
size of all 600 clips in the build is modest: at mono, optimized sample rate,
mid quality, expect roughly **15–25 MB** of AAC — fine inside the initial
download at this scale.

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
| Preload Audio Data | **Off** | The pairing that makes Decompress On Load safe: decode per use, not 600 at startup |
| Quality | **50** to start | A/B test 35 / 50 / 65 on real lines; ship the lowest that passes on laptop/Chromebook speakers |
| Sample Rate | Optimize | Free size win for speech |
| Packaging | **Resources folder is fine at 600 clips** (~15–25 MB in the build) | Addressables/AssetBundles only pay off if the initial download gets too big, or you want dialog updates without rebuilding — worth keeping in the back pocket given the Chromebook PWA delivery plans, not needed day one |

## To verify in a real Web build (not the Editor)

1. **First-line latency**: start a conversation whose clip wasn't loaded yet —
   does audio start with the text, or lag it? (If it lags: warm the next
   conversation's clips slightly early rather than preloading everything.)
2. **Rapid advancing/skipping** through lines — `AudioWait()` should stop the
   old line cleanly; watch for decode churn.
3. **Memory after several long conversations** (browser task manager on an
   actual Chromebook): confirm clips are being released, not accumulating.
4. **The quality A/B**: three builds at 35/50/65 — compare total build size and
   clarity on the worst speakers anyone will use.
5. **`AudioClip.frequency` at runtime** if any code depends on it — the browser
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
  should fit inside?
