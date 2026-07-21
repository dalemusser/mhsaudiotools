#!/usr/bin/env python3
"""
generate_toppo_lessons.py — ElevenLabs voice generator for Toppo's lesson lines.

Reads toppo-lessons.txt (one line per lesson, "id: spoken text") and, for each
line, calls ElevenLabs text-to-speech in Toppo's voice, writing an MP3 named
after the line's id into ./toppo-lessons/.

Run from the audiotools/ folder with the project venv:
    .env/bin/python generate_toppo_lessons.py --dry-run
    .env/bin/python generate_toppo_lessons.py
    .env/bin/python generate_toppo_lessons.py --force

The API key comes from ELEVENLABS_API_KEY (env var, or secrets.env / ~/.elevenlabs_key).
See CLAUDE.md / generate_ceremony_audio.py for the shared conventions.
"""

import argparse
import csv
import hashlib
import json
import os
import sys
import time
from pathlib import Path

# --- Fixed config ------------------------------------------------------------

HERE = Path(__file__).resolve().parent
LESSONS_TXT = HERE / "toppo-lessons.txt"
VOICES_CSV = HERE / "VoiceAssignments.csv"
OUT_DIR = HERE / "toppo-lessons"
MANIFEST = OUT_DIR / "toppo_lessons.json"

SPEAKER = "Toppo"                            # whose voice these lines use
MODEL_ID = "eleven_multilingual_v2"          # highest quality
OUTPUT_FORMATS = ["mp3_44100_192", "mp3_44100_128"]  # try best first, then fall back

MAX_RETRIES = 4
BACKOFF_BASE = 2.0  # seconds: 2, 4, 8, ...


# --- Input parsing -----------------------------------------------------------

def toppo_voice_id(csv_path: Path) -> str:
    """Look up Toppo's Voice ID from VoiceAssignments.csv."""
    if not csv_path.exists():
        sys.exit(f"ERROR: voice assignments not found: {csv_path}")
    with csv_path.open(newline="") as f:
        for row in csv.DictReader(f):
            # CSV headers carry trailing colons: "Character:", "Voice ID:".
            if (row.get("Character:") or "").strip() == SPEAKER:
                vid = (row.get("Voice ID:") or "").strip()
                if vid:
                    return vid
    sys.exit(f"ERROR: no Voice ID for '{SPEAKER}' in {csv_path}")


def parse_lessons(txt_path: Path) -> list:
    """
    Parse "id: text" lines. Blank lines are skipped; duplicate ids get a numeric
    suffix (id_2, id_3, ...) so filenames stay unique. Returns a list of dicts:
    {id, text, filename, text_hash, char_count, lineno}.
    """
    if not txt_path.exists():
        sys.exit(f"ERROR: lessons file not found: {txt_path}")

    lessons = []
    seen = {}  # base id -> count
    for lineno, raw in enumerate(txt_path.read_text(encoding="utf-8").splitlines(), 1):
        line = raw.strip()
        if not line:
            continue
        if ":" not in line:
            print(f"  WARN: line {lineno} has no ':' separator; skipping -> {line[:60]!r}")
            continue
        base_id, _, text = line.partition(":")
        base_id = base_id.strip()
        text = text.strip()
        if not base_id:
            print(f"  WARN: line {lineno} has an empty id; skipping")
            continue
        if not text:
            print(f"  WARN: line {lineno} (id {base_id}) has no text; skipping")
            continue

        seen[base_id] = seen.get(base_id, 0) + 1
        if seen[base_id] == 1:
            file_id = base_id
        else:
            file_id = f"{base_id}_{seen[base_id]}"
            print(f"  WARN: duplicate id '{base_id}' (line {lineno}); "
                  f"naming this file '{file_id}'")

        lessons.append({
            "id": file_id,
            "text": text,
            "filename": f"{file_id}.mp3",
            "text_hash": hashlib.sha1(text.encode("utf-8")).hexdigest(),
            "char_count": len(text),
            "lineno": lineno,
        })
    return lessons


# --- Synthesis ---------------------------------------------------------------

def synthesize(client, lesson: dict, voice_id: str) -> bytes:
    """Call ElevenLabs TTS; return MP3 bytes. Retries with backoff, format fallback."""
    last_err = None
    for fmt in OUTPUT_FORMATS:
        for attempt in range(1, MAX_RETRIES + 1):
            try:
                audio = client.text_to_speech.convert(
                    voice_id=voice_id,
                    text=lesson["text"],
                    model_id=MODEL_ID,
                    output_format=fmt,
                )
                # convert() may return raw bytes or a generator of chunks.
                if isinstance(audio, (bytes, bytearray)):
                    return bytes(audio)
                return b"".join(audio)
            except Exception as e:  # noqa: BLE001 — SDK raises various API errors
                last_err = e
                msg = str(e)
                # Unknown voice / auth problems won't fix themselves: don't retry.
                if any(s in msg.lower() for s in
                       ("voice", "unauthor", "invalid_api_key", "401", "404")):
                    if "format" not in msg.lower():
                        raise
                if attempt < MAX_RETRIES:
                    wait = BACKOFF_BASE ** attempt
                    print(f"    retry {attempt}/{MAX_RETRIES - 1} for {lesson['id']} "
                          f"after error ({msg[:80]}…); waiting {wait:.0f}s")
                    time.sleep(wait)
        print(f"    format {fmt} failed for {lesson['id']}; trying fallback format")
    raise RuntimeError(f"all formats/retries failed for {lesson['id']}: {last_err}")


# --- Key + manifest ----------------------------------------------------------

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
                    return line
    return ""


def load_manifest() -> dict:
    if MANIFEST.exists():
        try:
            return json.loads(MANIFEST.read_text())
        except (json.JSONDecodeError, OSError):
            print(f"  WARN: could not read existing manifest {MANIFEST}; rebuilding")
    return {"items": []}


def write_manifest(by_id: dict):
    items = [by_id[k] for k in by_id]
    MANIFEST.write_text(json.dumps({"items": items}, indent=2, ensure_ascii=False) + "\n")


# --- Main --------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser(
        description="Generate ElevenLabs audio for Toppo's lesson lines.",
    )
    ap.add_argument("--dry-run", action="store_true",
                    help="List what would be generated (no API calls).")
    ap.add_argument("--force", action="store_true",
                    help="Regenerate every line, ignoring existing audio/hashes.")
    args = ap.parse_args()

    voice_id = toppo_voice_id(VOICES_CSV)
    lessons = parse_lessons(LESSONS_TXT)
    if not lessons:
        sys.exit("ERROR: no lesson lines found in toppo-lessons.txt")

    total_chars = sum(l["char_count"] for l in lessons)
    print(f"Lessons file:  {LESSONS_TXT}")
    print(f"Output dir:    {OUT_DIR}")
    print(f"Voice ({SPEAKER}):  {voice_id}")
    print(f"Lines:         {len(lessons)}  (total {total_chars} chars)\n")

    if args.dry_run:
        print("DRY RUN — would generate:")
        for l in lessons:
            print(f"  {l['id']:<16} {l['char_count']:>4} chars  -> {l['filename']}")
        print("\n(no API calls made)")
        return

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
    by_id = {it["id"]: it for it in manifest.get("items", [])}

    generated = skipped = 0
    for l in lessons:
        mp3_path = OUT_DIR / l["filename"]
        prior = by_id.get(l["id"])
        up_to_date = (
            not args.force
            and mp3_path.exists()
            and prior is not None
            and prior.get("textHash") == l["text_hash"]
        )
        if up_to_date:
            print(f"  {l['id']:<16} up-to-date, skipping")
            skipped += 1
            continue

        print(f"  {l['id']:<16} generating ({l['char_count']} chars)…")
        audio = synthesize(client, l, voice_id)
        mp3_path.write_bytes(audio)
        by_id[l["id"]] = {
            "id": l["id"],
            "speaker": SPEAKER,
            "audio": f"toppo-lessons/{l['filename']}",
            "textHash": l["text_hash"],
            "sourceLine": l["lineno"],
        }
        print(f"      wrote {l['filename']}  ({len(audio)} bytes)")
        generated += 1

    write_manifest(by_id)

    print(f"\nSummary: {len(lessons)} lines — {generated} generated, {skipped} skipped, "
          f"{total_chars} total chars")
    print(f"Manifest: {MANIFEST}")


if __name__ == "__main__":
    main()
