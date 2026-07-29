> **Superseded** by [unity-webgl-audio-import.md](unity-webgl-audio-import.md) (2026-07-28), which is verified against current Unity Web docs and sized for our ~6,000-clip case. Kept for history.

You’re thinking about it the right way. If ElevenLabs is giving you MP3s, converting them to WAV yourself does not recover any lost quality. All it does is wrap the already-lossy audio in an uncompressed container. Unity is going to decode the MP3 to PCM internally anyway, so your manual conversion is essentially duplicating the first step of Unity’s import pipeline.

In other words:

ElevenLabs MP3
      ↓
Unity decodes to PCM
      ↓
Unity compresses for WebGL

versus

ElevenLabs MP3
      ↓
You convert to WAV
      ↓
Unity decodes the WAV (already PCM)
      ↓
Unity compresses for WebGL

The final quality is essentially the same because the information lost in the original MP3 cannot be restored.

⸻

Unity Audio Import Settings

Select an audio file in the Project window and you’ll see the AudioClip Import Settings in the Inspector.

The important settings are:

Load Type

This determines when and how the audio is loaded.

Decompress On Load

Disk
   ↓
Compressed
   ↓
Memory
   ↓
PCM

Pros

* Instant playback
* Lowest CPU usage
* Best for small sounds

Cons

* Uses the most RAM

Typical use

* Button clicks
* UI sounds
* Footsteps
* Gunshots
* Short voice clips

⸻

Compressed In Memory

Disk
   ↓
Compressed
   ↓
Memory (still compressed)
   ↓
Decoded while playing

Pros

* Lower RAM usage

Cons

* Slight CPU cost during playback

Typical use

* Medium-length voice clips
* Ambient sounds
* Longer sound effects

⸻

Streaming

Disk
      ↓
Small buffer
      ↓
Play while downloading from file

Pros

* Very little RAM

Cons

* Slight startup delay
* More disk access

Typical use

* Background music
* Long narration
* Podcasts
* Cutscene dialogue

For WebGL, note that “streaming” behaves differently than on desktop because all game assets are downloaded into the browser first. It still reduces memory usage, but there isn’t true on-demand disk streaming in the same way as a native application.

⸻

Compression Format

This controls the codec Unity uses for the target platform.

You’ll typically see options like:

* PCM
* ADPCM
* Vorbis

PCM

No compression.

Pros

* Highest quality
* No decoding cost

Cons

* Huge

Only use for tiny sounds.

⸻

ADPCM

Designed for game sound effects.

Pros

* Fast
* Small CPU cost
* Better than PCM size

Cons

* Not ideal for music

Great for:

* Explosions
* Doors
* UI sounds

⸻

Vorbis

Best compression.

Pros

* Very small
* Excellent music quality

Cons

* More CPU to decode

Best for:

* Music
* Speech
* Ambient sounds

⸻

Quality Slider

Only affects Vorbis.

Lower

Small download
Lower fidelity

Higher

Large download
Higher fidelity

Typical values

Voice

40–60

Music

50–70

High-end music

70–90

Going above about 80 often produces diminishing returns unless the source is especially demanding.

⸻

Sample Rate Setting

Usually leave this on Optimize Sample Rate.

Unity analyzes the clip and may reduce:

48 kHz

to

24 kHz

if it won’t noticeably affect quality.

This can significantly reduce build size.

If you’re using synthesized speech from ElevenLabs, optimizing or preserving the sample rate are both reasonable choices. Voice generally doesn’t benefit much from very high sample rates.

⸻

Force To Mono

Highly recommended for many sounds.

Stereo

Left
Right

↓

Mono

Single channel

Cuts file size roughly in half.

Don’t use it for:

* Music
* Stereo ambience
* Positional effects that rely on stereo imaging

Use it for:

* Voice
* UI
* Most sound effects

⸻

Normalize

If available, Unity can normalize the clip.

Usually leave it off unless your assets vary widely in loudness. For ElevenLabs output, you’re often better off controlling levels during content creation or with an AudioMixer.

⸻

Background Loading

Allows Unity to load the clip asynchronously.

Useful for larger clips that don’t need to be available immediately.

⸻

Preload Audio Data

If checked:

Everything loads when the scene loads.

If unchecked:

The clip loads when first needed.

For a game with lots of voice lines, disabling preload on clips that aren’t needed immediately can reduce startup time and memory usage.

⸻

Platform Overrides

One of Unity’s best features is that you can have different settings per platform.

For example:

Platform	Format	Quality
Windows	PCM	N/A
Android	Vorbis	45
iOS	AAC (platform-specific)	~50
WebGL	Vorbis	50

So your editor may use high-quality settings, while WebGL gets a more size-efficient encoding.

⸻

For an ElevenLabs-heavy WebGL game

If your game has lots of spoken dialogue, I’d start with something like this:

Setting	Recommendation
Source	Use the MP3 directly from ElevenLabs
Load Type	Decompress On Load for short lines; Compressed In Memory for longer dialogue
Compression	Vorbis
Quality	45–55
Sample Rate	Optimize Sample Rate
Force To Mono	Yes (unless you intentionally created stereo dialogue)
Preload Audio Data	Only for clips needed immediately

One last thought: if you’re generating a large amount of AI speech, it’s worth experimenting with the Quality slider on a representative set of clips. Spoken dialogue is much more forgiving than music, and many developers find they can reduce the quality into the 40–50 range with little perceptible difference while substantially reducing the WebGL download size.