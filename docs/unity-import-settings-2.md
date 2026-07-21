That actually changes my recommendations quite a bit. A dialogue system has a very different usage pattern than a game with lots of overlapping sound effects.

From what you’ve described:

Dialogue text appears
        ↓
Pixel Crushers Dialogue System
        ↓
Looks up AudioClip by ID
        ↓
Plays clip once
        ↓
Moves to next line

Most clips are:

* one speaker
* one voice
* probably 1–10 seconds long
* played once
* rarely overlap more than one or two at a time

This is an ideal workload for optimizing memory.

I would use these settings

Setting	Recommendation	Why
Source	ElevenLabs MP3	No need to convert to WAV
Force To Mono	✔ Yes	Speech is inherently mono
Compression	Vorbis	Excellent speech compression
Quality	45–50	Speech remains very clear
Sample Rate	Optimize	Voice doesn’t need 44.1/48 kHz unless the source is exceptionally high quality
Load Type	Compressed In Memory	Good balance of memory and CPU
Preload Audio Data	Off	Don’t load every line of dialogue up front
Load In Background	On	Helps hide loading latency for larger clips

⸻

I would avoid “Decompress On Load”

Suppose you have:

* 3,000 dialogue clips
* average 4 seconds

That’s roughly

3,000 × 4 sec
= 12,000 seconds
≈ 3.3 hours of dialogue

If every clip is decompressed into PCM, the memory footprint becomes enormous.

Keeping them compressed in memory means only the compressed data is resident, and Unity decodes the small amount needed during playback. Since dialogue clips are short and only a few play simultaneously, the CPU overhead is usually negligible on modern hardware.

⸻

Force To Mono is one of the biggest wins

ElevenLabs speech is effectively mono.

Keeping it stereo just stores two nearly identical channels.

Mono immediately reduces the size by about 50%.

Players won’t notice unless you intentionally created stereo voice recordings.

⸻

Quality

For AI-generated speech, you can often go surprisingly low.

I’d test:

* 60
* 50
* 40

Blind A/B testing often shows that 40–50 is essentially indistinguishable from higher settings for spoken dialogue, especially in the context of a game.

⸻

Sample Rate

Speech has much lower frequency content than music.

Unity’s “Optimize Sample Rate” often chooses something like 22.05 kHz or 24 kHz for voice without an audible difference, which can significantly reduce file sizes.

⸻

One thing I’d investigate with Pixel Crushers

Pixel Crushers’ Dialogue System has several ways to handle voice-over assets. Depending on how you’re using it, you may not need thousands of AudioClip references loaded into memory.

Some teams use:

* Addressables
* AssetBundles
* asynchronous loading

so that dialogue clips are loaded just before a conversation and unloaded afterward. If your game has a lot of spoken content, this can dramatically reduce the browser’s memory usage in WebGL.

A question

Approximately how much dialogue are you expecting?

For example:

* 100 lines
* 1,000 lines
* 10,000+ lines

The answer changes how aggressively I’d optimize. At around 10,000+ voice clips, I’d start thinking not just about import settings but also about how the dialogue audio is packaged and loaded, because that can have a much larger impact on WebGL performance and memory than tweaking the Vorbis quality from 50 to 60.

At 5,000 dialogue clips, I would revise the emphasis:

Keep the ElevenLabs MP3 files as your Unity source assets. Unity’s current Web platform documentation says Web audio clips are imported into the build as AAC, so converting MP3 to WAV first would only make the project files much larger; it would not restore quality. The effective path is MP3 → decoded audio → Unity’s Web AAC output.  

Recommended import preset for your dialogue

Setting	Starting point
Force To Mono	On, after verifying the MP3s are genuinely mono-compatible
Load Type	Compressed In Memory
Compression Format	Use the Web platform’s compressed option
Quality	Around 45–55
Sample Rate Setting	Optimize Sample Rate initially
Preload Audio Data	Off
Load In Background	On

Important WebGL clarification

My earlier mention of Vorbis as the final WebGL format was not accurate for Unity Web builds. Unity currently converts Web AudioClip assets to AAC, regardless of whether the source was MP3, WAV, or OGG.  

The Inspector may still present generic audio compression controls, but the Web build pipeline ultimately produces the browser-compatible representation Unity documents.

The bigger issue: Resources

Pixel Crushers supports naming clips by dialogue entry ID and resolving them with something such as:

AudioWait(entrytag)

Its documentation says those clips can be placed in a Resources folder, marked Addressable, or put into an AssetBundle.  

For a few hundred clips, Resources is convenient. For 5,000 clips, I would be cautious about putting the entire voice library into one Resources folder and therefore into the initial Web build.

Even with Preload Audio Data disabled, those assets can still:

* increase the initial downloadable build considerably;
* add thousands of asset records;
* consume browser-managed memory and Unity heap space;
* make content updates require rebuilding or redownloading a large package;
* lengthen build and import times.

Preload Audio Data = Off does not mean the audio file magically stays on your web server as an individually downloadable MP3. If the clip is part of the main Unity data file, its compressed data is still part of that downloaded build package.

Better packaging structure

I would divide the dialogue into logical groups, such as:

Core/tutorial dialogue
Chapter 1 dialogue
Chapter 2 dialogue
Location A dialogue
Location B dialogue
Optional conversations

Then package those groups through Addressables or AssetBundles, loading only the relevant group before entering that chapter or location and releasing it afterward. Pixel Crushers explicitly supports voice clips in AssetBundles or as Addressables rather than requiring everything under Resources.  

For Web builds, remote AssetBundles can reduce the initial download and let the browser fetch dialogue content when needed. Unity specifically points to AssetBundles as a way to reduce initial Web application load times.  

Why Compressed In Memory still makes sense

For short sequential dialogue:

* only one clip usually plays at a time;
* decoding cost is modest;
* keeping clips decompressed as PCM would waste memory;
* dialogue does not generally require the ultra-low latency of UI clicks or weapon sounds.

Therefore, Compressed In Memory is a sensible starting choice. I would not use Decompress On Load globally across 5,000 clips.

As an illustration, suppose the average line is five seconds. Five thousand lines would be nearly seven hours of audio. At mono 44.1 kHz, 16-bit PCM, the decoded audio would be roughly:

5,000 × 5 seconds × 44,100 samples × 2 bytes
≈ 2.2 GB

That is before asset and runtime overhead. Your AAC build data will be vastly smaller, but it shows why avoiding broad PCM decompression matters.

Preload Audio Data and Pixel Crushers lookup

I would test this specific interaction carefully:

1. Set the voice clip to Preload Audio Data = Off.
2. Begin a conversation that references it through AudioWait(entrytag).
3. Test the first line in an actual Web build—not only in the Editor.
4. Check whether the line starts immediately or whether the dialogue text appears before the audio is decoded.
5. Test rapid skipping and advancing.

AudioWait() is preferable to a fire-and-forget Audio() call for this use because Pixel Crushers says AudioWait() coordinates dialogue progression with playback and can stop playback when the user advances early.  

You may find that Load In Background plus no preload causes the first line in a newly loaded group to hesitate. The usual solution is to preload the relevant bundle or clip group shortly before the conversation starts—not to preload all 5,000 clips at startup.

My practical recommendation

Use the ElevenLabs MP3s directly and create a Unity importer preset like:

Dialogue Voice – Web
Force To Mono: On
Load Type: Compressed In Memory
Quality: 50
Sample Rate: Optimize
Preload Audio Data: Off
Load In Background: On

Then create three test Web builds using quality levels 35, 50, and 65. Compare:

* total .data or bundle size;
* first-line startup delay;
* voice clarity through typical laptop speakers;
* memory use after several conversations;
* behavior when the player rapidly skips lines.

Most importantly, avoid treating all 5,000 clips as one permanently loaded collection. Organizing them into chapter-, scene-, character-, or conversation-level Addressable groups will probably yield a larger improvement than changing the quality slider by ten points.