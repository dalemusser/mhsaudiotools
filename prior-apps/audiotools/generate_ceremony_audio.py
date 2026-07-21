#!/usr/bin/env python3
"""
generate_ceremony_audio.py — ElevenLabs voice + word-timing generator for the
MHS end-of-game ceremony.

For every spoken beat in ../babylon/ceremony-script.js this:
  1. calls ElevenLabs text-to-speech *with timestamps* in the speaker's assigned
     voice, writing an MP3 into ../babylon/assets/audio/, and
  2. derives word-level timings from the character alignment and records them in
     ../babylon/assets/audio/ceremony_audio.json,

so the ceremony caption can be karaoke-highlighted in sync with the real audio.

Run from the audiotools/ folder with the project venv:
    .env/bin/python generate_ceremony_audio.py --dry-run
    .env/bin/python generate_ceremony_audio.py
    .env/bin/python generate_ceremony_audio.py --force

See CLAUDE.md for the full spec.
"""

import argparse
import base64
import csv
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

# --- Fixed config (see CLAUDE.md "Decisions already made") -------------------

HERE = Path(__file__).resolve().parent
SCRIPT_JS = (HERE / ".." / "babylon" / "ceremony-script.js").resolve()
VOICES_CSV = HERE / "VoiceAssignments.csv"
OUT_DIR = (HERE / ".." / "babylon" / "assets" / "audio").resolve()
MANIFEST = OUT_DIR / "ceremony_audio.json"

# Only these characters speak in the ceremony; ignore the rest of the CSV.
CEREMONY_CHARACTERS = {"Toppo", "Tera", "Anderson", "Aryn", "Jasper"}

MODEL_ID = "eleven_multilingual_v2"          # highest quality, returns timestamps
OUTPUT_FORMATS = ["mp3_44100_192", "mp3_44100_128"]  # try best first, then fall back

MAX_RETRIES = 4
BACKOFF_BASE = 2.0  # seconds: 2, 4, 8, ...


# --- Input extraction --------------------------------------------------------

def extract_beats(script_path: Path) -> list:
    """Run the JS through Node to get CEREMONY_SCRIPT.beats as real data."""
    if not script_path.exists():
        sys.exit(f"ERROR: ceremony script not found: {script_path}")
    node = (
        "global.window = {};"
        f"require({json.dumps(str(script_path))});"
        "process.stdout.write(JSON.stringify(window.CEREMONY_SCRIPT));"
    )
    try:
        out = subprocess.run(
            ["node", "-e", node],
            check=True, capture_output=True, text=True,
        ).stdout
    except FileNotFoundError:
        sys.exit("ERROR: `node` not found on PATH; needed to parse ceremony-script.js")
    except subprocess.CalledProcessError as e:
        sys.exit(f"ERROR: Node failed to load the ceremony script:\n{e.stderr}")
    data = json.loads(out)
    beats = data.get("beats")
    if not isinstance(beats, list):
        sys.exit("ERROR: CEREMONY_SCRIPT.beats is missing or not an array")
    return beats


def load_voice_map(csv_path: Path) -> dict:
    """Map ceremony Character -> Voice ID (first match wins)."""
    if not csv_path.exists():
        sys.exit(f"ERROR: voice assignments not found: {csv_path}")
    voices = {}
    with csv_path.open(newline="") as f:
        for row in csv.DictReader(f):
            # CSV headers have trailing colons: "Character:", "Voice ID:".
            char = (row.get("Character:") or "").strip()
            vid = (row.get("Voice ID:") or "").strip()
            if char in CEREMONY_CHARACTERS and char not in voices and vid:
                voices[char] = vid
    missing = CEREMONY_CHARACTERS - voices.keys()
    if missing:
        sys.exit(f"ERROR: no voice ID for ceremony character(s): {', '.join(sorted(missing))}")
    return voices


def spoken_lines(beats: list, voices: dict) -> list:
    """Return [{index, speaker, text, voice_id, filename, text_hash, char_count}] for spoken beats."""
    lines = []
    for index, beat in enumerate(beats):
        if beat.get("type") == "celebration":
            continue
        speaker = beat.get("speaker")
        text = beat.get("text")
        if not speaker or not text:
            continue
        if speaker not in voices:
            sys.exit(f"ERROR: beat {index} speaker '{speaker}' has no ceremony voice assignment")
        text_hash = hashlib.sha1(text.encode("utf-8")).hexdigest()
        lines.append({
            "index": index,
            "speaker": speaker,
            "text": text,
            "voice_id": voices[speaker],
            "filename": f"ceremony_{index:02d}_{speaker}.mp3",
            "text_hash": text_hash,
            "char_count": len(text),
        })
    return lines


# --- Word timings from character alignment -----------------------------------

def words_from_alignment(alignment) -> list:
    """
    Walk the character-level alignment and split into words on whitespace,
    matching the ceremony's text.trim().split(/\\s+/) tokenization. Each word's
    start = the start time of its first character. Returns [[startSec, word], ...].
    """
    chars = alignment.characters
    starts = alignment.character_start_times_seconds
    words = []
    cur_chars = []
    cur_start = None
    for ch, st in zip(chars, starts):
        if ch.isspace():
            if cur_chars:
                words.append([round(cur_start, 3), "".join(cur_chars)])
                cur_chars, cur_start = [], None
        else:
            if not cur_chars:
                cur_start = st
            cur_chars.append(ch)
    if cur_chars:
        words.append([round(cur_start, 3), "".join(cur_chars)])
    return words


def caption_tokens(text: str) -> list:
    """Reproduce the ceremony's text.trim().split(/\\s+/)."""
    t = text.strip()
    return re.split(r"\s+", t) if t else []


# --- Synthesis ---------------------------------------------------------------

def synthesize(client, line: dict):
    """Call ElevenLabs with timestamps; return (audio_bytes, words, duration_sec)."""
    last_err = None
    for fmt in OUTPUT_FORMATS:
        for attempt in range(1, MAX_RETRIES + 1):
            try:
                resp = client.text_to_speech.convert_with_timestamps(
                    voice_id=line["voice_id"],
                    text=line["text"],
                    model_id=MODEL_ID,
                    output_format=fmt,
                )
                audio = base64.b64decode(resp.audio_base_64)
                align = resp.alignment
                words = words_from_alignment(align)
                ends = align.character_end_times_seconds
                duration = round(ends[-1], 3) if ends else 0.0

                # Sanity-check tokenization against the caption splitter.
                expected = caption_tokens(line["text"])
                if len(words) != len(expected):
                    print(f"  WARN: beat {line['index']} ({line['speaker']}): "
                          f"{len(words)} aligned words vs {len(expected)} caption tokens "
                          f"(normalization drift; sync may be approximate)")
                return audio, words, duration
            except Exception as e:  # noqa: BLE001 — SDK raises various API errors
                last_err = e
                msg = str(e)
                # Unknown voice / auth problems won't fix themselves: don't retry.
                if any(s in msg.lower() for s in ("voice", "unauthor", "invalid_api_key", "401", "404")):
                    # but a bad output_format should fall through to the fallback fmt
                    if "format" not in msg.lower():
                        raise
                if attempt < MAX_RETRIES:
                    wait = BACKOFF_BASE ** attempt
                    print(f"  retry {attempt}/{MAX_RETRIES - 1} for beat {line['index']} "
                          f"after error ({msg[:80]}…); waiting {wait:.0f}s")
                    time.sleep(wait)
        # exhausted retries for this format -> try the next (fallback) format
        print(f"  format {fmt} failed for beat {line['index']}; trying fallback format")
    raise RuntimeError(f"all formats/retries failed for beat {line['index']}: {last_err}")


# --- Manifest ----------------------------------------------------------------

def resolve_api_key() -> str:
    """Env var first; then git-ignored file fallbacks (never .env — that's the venv)."""
    key = os.environ.get("ELEVENLABS_API_KEY")
    if key:
        return key.strip()
    for path in (HERE / "secrets.env", Path.home() / ".elevenlabs_key"):
        if path.exists():
            for raw in path.read_text().splitlines():
                line = raw.strip()
                if not line or line.startswith("#"):
                    continue
                if "=" in line:
                    name, _, val = line.partition("=")
                    if name.strip() == "ELEVENLABS_API_KEY":
                        return val.strip().strip('"').strip("'")
                else:
                    return line  # raw key on its own line (~/.elevenlabs_key)
    return ""


def load_manifest() -> dict:
    if MANIFEST.exists():
        try:
            return json.loads(MANIFEST.read_text())
        except (json.JSONDecodeError, OSError):
            print(f"  WARN: could not read existing manifest {MANIFEST}; rebuilding")
    return {"items": []}


def write_manifest(items: list):
    items = sorted(items, key=lambda it: it["index"])
    MANIFEST.write_text(json.dumps({"items": items}, indent=2, ensure_ascii=False) + "\n")


# --- Main --------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser(
        description="Generate ElevenLabs ceremony audio + word timings.",
    )
    ap.add_argument("--dry-run", action="store_true",
                    help="List what would be generated (no API calls).")
    ap.add_argument("--force", action="store_true",
                    help="Regenerate every line, ignoring existing audio/hashes.")
    args = ap.parse_args()

    beats = extract_beats(SCRIPT_JS)
    voices = load_voice_map(VOICES_CSV)
    lines = spoken_lines(beats, voices)

    if not lines:
        sys.exit("ERROR: no spoken lines found in ceremony script")

    total_chars = sum(l["char_count"] for l in lines)
    print(f"Ceremony script: {SCRIPT_JS}")
    print(f"Output dir:      {OUT_DIR}")
    print(f"Spoken lines:    {len(lines)}  (total {total_chars} chars)\n")

    if args.dry_run:
        print("DRY RUN — would generate:")
        for l in lines:
            print(f"  [{l['index']:>2}] {l['speaker']:<9} voice={l['voice_id']}  "
                  f"{l['char_count']:>4} chars  -> {l['filename']}")
        print("\n(no API calls made)")
        return

    # Real run.
    from elevenlabs.client import ElevenLabs

    api_key = resolve_api_key()
    if not api_key:
        sys.exit(
            "ERROR: ElevenLabs API key not found. Set ELEVENLABS_API_KEY in the "
            "environment, or put the key in audiotools/secrets.env "
            "(ELEVENLABS_API_KEY=...) or ~/.elevenlabs_key."
        )
    client = ElevenLabs(api_key=api_key)

    OUT_DIR.mkdir(parents=True, exist_ok=True)
    manifest = load_manifest()
    by_index = {it["index"]: it for it in manifest.get("items", [])}

    generated = skipped = 0
    for l in lines:
        mp3_path = OUT_DIR / l["filename"]
        prior = by_index.get(l["index"])
        up_to_date = (
            not args.force
            and mp3_path.exists()
            and prior is not None
            and prior.get("textHash") == l["text_hash"]
        )
        if up_to_date:
            print(f"  [{l['index']:>2}] {l['speaker']:<9} up-to-date, skipping")
            skipped += 1
            continue

        print(f"  [{l['index']:>2}] {l['speaker']:<9} generating ({l['char_count']} chars)…")
        audio, words, duration = synthesize(client, l)
        mp3_path.write_bytes(audio)
        by_index[l["index"]] = {
            "index": l["index"],
            "speaker": l["speaker"],
            "audio": f"assets/audio/{l['filename']}",
            "durationSec": duration,
            "words": words,
            "textHash": l["text_hash"],
        }
        print(f"        wrote {l['filename']}  ({duration:.2f}s, {len(words)} words)")
        generated += 1

    write_manifest(list(by_index.values()))

    print(f"\nSummary: {len(lines)} lines — {generated} generated, {skipped} skipped, "
          f"{total_chars} total chars")
    print(f"Manifest: {MANIFEST}")


if __name__ == "__main__":
    main()
