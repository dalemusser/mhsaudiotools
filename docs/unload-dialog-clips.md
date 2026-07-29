# Unloading dialog clips: the release problem and what to do about it

*Companion to [unity-webgl-audio-import.md](unity-webgl-audio-import.md) and
[unity-addressables-dialog-audio.md](unity-addressables-dialog-audio.md).
Open question for the team: **does anything in the current builds unload
dialog clips between scene changes?** This doc explains why that matters and
what to implement if the answer is no.*

## The issue

Loading is not the problem — with Preload Audio Data off, the Dialogue
System's entrytag lookup loads each clip on demand when its line plays.
**Release is the problem.** A clip loaded from a `Resources` folder stays in
memory after the line finishes; Unity keeps it cached until something calls
`Resources.UnloadUnusedAssets()` (which Unity runs automatically on a
non-additive scene load) or unloads it explicitly.

What lingers is the expensive form: with **Decompress On Load** (the right
Web setting for dialog), the cached clip holds its **decoded** audio — float32
PCM, roughly **0.5–1 MB per 4-second mono line** at the browser's 48 kHz. So
during a conversation-heavy stretch inside one scene:

```
lines played since last scene change × ~0.5–1 MB  =  lingering memory
```

A few dozen lines is noise. A few *hundred* lines without a scene change is
100–300 MB of dead decoded audio on a Chromebook — the "stair-step" pattern
in the browser task manager. Whether this bites depends entirely on the
game's rhythm: if every conversation cluster is followed soon by a scene
load, Unity's built-in sweep already bounds it and **no action is needed**.

## What Addressables gives automatically

Addressable-loaded clips are **reference-counted**, and the Dialogue System
releases them **when the sequencer command ends** — per line, automatically,
no hooks, no sweeps, no accumulation. This is a genuine (if modest) point in
Addressables' favor; it is *not* by itself a reason to adopt them, because
the build-resident world can match it with one small script:

## Recommendation if Addressables are NOT used

**Add a conversation-end unload hook** — a script on the Dialogue Manager:

```csharp
using UnityEngine;

// Frees dialog AudioClips (and anything else unreferenced) after each
// conversation. OnConversationEnd is a standard Dialogue System message
// sent to the Dialogue Manager and participants.
public class UnloadDialogAudio : MonoBehaviour
{
    void OnConversationEnd(Transform actor)
    {
        Resources.UnloadUnusedAssets();
    }
}
```

Why conversation end, not per line: `UnloadUnusedAssets()` is a global sweep
over all loaded objects and can cause a brief hitch — a conversation's end is
a natural pause where a hitch is invisible; per line it would stutter
constantly. Replaying a conversation later is cheap: the compressed bytes are
still in the (memory-resident) build data, so a reload is just a re-decode,
milliseconds per line.

**Refinement only if profiling demands it:** if the sweep hitch is measurable
on Chromebooks (big scenes make the sweep slower), the surgical alternative is
`Resources.UnloadAsset(clip)` on just the clip that finished, via a small
subclassed audio sequencer command. More code, zero sweep — hold this in
reserve; start with the one-liner.

## How to verify (real Web build, on a Chromebook)

1. Open the browser task manager; note the tab's memory.
2. Play several long conversations **without changing scenes**; watch for the
   stair-step climb (~0.5–1 MB per line).
3. With the hook in place, memory should drop back at each conversation end.

## For the meeting

- Does anything call `Resources.UnloadUnusedAssets()` today other than scene
  loads? Any custom asset caching that would object to the sweep?
- What's the longest dialog-dense stretch the game has **within a single
  scene**? That number × ~1 MB is the exposure.
- If the answer is "conversations are always near scene changes," write that
  down and skip the hook — bounded by design beats code.
