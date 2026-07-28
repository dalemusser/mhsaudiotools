# Code review — 2026-07-27

Full-project review of the engine, CLI, Wails app, frontend, and release
plumbing. Method: four parallel focused review passes (engine, CLI+plumbing,
Wails backend, frontend), findings then hand-verified against the code; the
top engine findings were reproduced empirically against the real packages.

Severity: **high** = costs real API money or loses work; **medium** = silent
wrong behavior; **low** = cosmetic, rare-input, or latent.

**All issues below were fixed the same day.** Every fix landed with the
described approach; new tests cover R1, R2, R4, R5, R7, R8, R11, R20, R21, and
R22 (both modules build, vet, and pass `go test -race`). Two notes:

- **R1/R2 (manifest):** entries now record text hash + voice + model + format +
  timings flag; legacy bare-hash manifests still load with the missing fields
  matching anything, so upgrading does **not** regenerate existing folders. The
  manifest is flushed every 25 written files and saved via temp+rename, and a
  corrupt manifest now fails the run loudly instead of silently rebuilding.
- **R13 (emotion editor):** the save now reports skipped and duplicate rows;
  alphabetical reordering on reopen remains, as accepted (the file format keys
  tags by phrase).

---

## High

### R1 — Resume manifest keys on text only; voice/model/format changes never regenerate
- **Where:** `engine/job/job.go` (`hashText(it.Text)` at plan/execute; manifest maps rel → text hash)
- **What:** Changing a character's voice, flipping a voice v2↔v3, switching the
  audio format (same `.mp3` relpath), or enabling `-timestamps` later leaves
  every byte-identical line "up to date" — stale audio stays on disk and
  `.words.json` sidecars are never produced. Only recourse is Force (re-pays the
  whole batch). Reproduced.
- **Fix:** manifest entries record text hash + voice + model + format +
  timestamps flag; up-to-date requires all to match. Legacy string-hash entries
  are grandfathered (text match only) so existing output folders don't mass-regenerate.
- **Status:** fixed

### R2 — Manifest saved only at end of run, non-atomically; corrupt manifest silently regenerates everything
- **Where:** `engine/job/job.go` (`man.save` after `execute` returns; plain
  `os.WriteFile`; unmarshal failure → silently empty manifest)
- **What:** A crash/kill/power-loss mid-batch loses the record of everything
  already written (thousands of paid files re-bought next run). A torn final
  write leaves truncated JSON that is silently treated as empty → full
  re-synthesis, no warning.
- **Fix:** periodic manifest flush during the run; write-temp-then-rename;
  corrupt manifest is a loud error telling the user to delete it deliberately.
- **Status:** fixed

### R3 — Closing the app window mid-run kills the process without saving the manifest
- **Where:** `wails/main.go` (no `OnBeforeClose`), `wails/app.go` Generate
- **What:** Wails v2.13 never cancels the app context on window close; the
  process dies wherever the worker pool happens to be, and (compounding R2) the
  manifest never lands. Verified against the Wails v2.13.0 source.
- **Fix:** `OnBeforeClose` hook cancels the active run and waits (bounded) for
  Generate to finish saving before the window closes.
- **Status:** fixed

## Medium

### R4 — Re-importing a VoiceAssignments.csv wipes per-character v2/v3 model choices
- **Where:** `engine/voice/store.go` `MergeFrom` (`c.Assignments = imported.Assignments`)
- **What:** The CSV has no model column, so every character's `Model` set in the
  voice editor silently resets to "" (v2 default) on re-import; player-slot
  models survive, making the asymmetry clearly unintended. Compounds with R1:
  the model reverts *and* the audio doesn't regenerate.
- **Fix:** carry each character's existing `Model` forward when the re-imported
  voice ID is unchanged (a new voice legitimately needs a fresh by-ear choice).
- **Status:** fixed

### R5 — Line IDs become file paths verbatim; a crafted ID escapes the output directory
- **Where:** `engine/output/output.go` (rel = ID + ext), `engine/job/job.go`
  (join + write, no containment check)
- **What:** A dbexport entrytag or simplescript ID containing `../` writes
  outside the chosen output folder (reproduced), overwriting whatever is there;
  `/` or `\` in IDs silently nests/collides.
- **Fix:** reject IDs containing path separators or `..` at plan time (surfaces
  in dry-run problems) and add a defense-in-depth containment check at write time.
- **Status:** fixed

### R6 — "Reveal output" button has never worked
- **Where:** `wails/app.go` `RevealOutput` (`BrowserOpenURL` with `file://`)
- **What:** Wails' URL validator hard-rejects the `file` scheme (verified in
  v2.13.0 source) and returns nothing — the button is a silent no-op.
- **Fix:** exec the platform opener (`open` / `explorer` / `xdg-open`) and
  return a real error on failure.
- **Status:** fixed

### R7 — A typo'd `-key-file` is silently ignored
- **Where:** `engine/keys/keys.go` `Resolve`
- **What:** A missing/unreadable/empty explicitly-passed key file is skipped and
  resolution falls through to `~/.elevenlabs_key` — a run can quietly spend the
  wrong account's credits, or fail with a message that never mentions the bad path.
- **Fix:** an explicitly named key file that can't be read (or is empty) is a
  loud error; env-var precedence unchanged.
- **Status:** fixed

### R8 — UTF-8 BOM: simplescript first ID is silently corrupted; voices CSV header detection derails
- **Where:** `engine/source/simplescript.go`, `engine/voice/csv.go`
- **What:** An Excel/Notepad-saved script gains an invisible `﻿` in the
  first line's ID → the generated filename never matches the game's lookup
  (reproduced). A BOM on a voices CSV header makes header detection fail and the
  header row becomes a junk assignment. `dbexport` already strips BOM; these don't.
- **Fix:** strip BOM in both parsers (Detect and Parse).
- **Status:** fixed

### R9 — CLI prints one concurrency and may run with another
- **Where:** `cmd/cli/generate.go` `reportConcurrency` + `engine/job/job.go` `concurrency`
- **What:** With `-concurrency 0`, the subscription API is called twice (report
  and pool-size are computed independently); if the second call flakes, the run
  silently drops to the conservative default (3) after printing e.g. "Parallel: 15".
- **Fix:** resolve once in the CLI, print it, and write the result into
  `Options.Concurrency` so the runner uses exactly what was reported.
- **Status:** fixed

### R10 — The app accepts an invalid API key silently
- **Where:** `wails/frontend/src/main.js` (SaveKey → loadAccount)
- **What:** SaveKey only writes the file; the follow-up account load swallows
  auth failure into "account unavailable" and the key card is already hidden.
  The user finds out at Generate time; the parallel slider stays at the HTML
  default max.
- **Fix:** validate the key after saving (account fetch); on failure keep the
  key card visible with the real error.
- **Status:** fixed

## Low

### R11 — Duplicate dbexport entrytags race two writers on one file
- **Where:** `engine/source/dbexport.go` (no `seen` map; contrast simplescript)
- **What:** Two rows sharing an entrytag → same RelPath synthesized twice
  concurrently; last write wins content, last set wins the manifest hash — they
  need not agree.
- **Fix:** duplicate entrytags are an error naming the tags (a corrupt export
  should be fixed at the source, not silently renamed).
- **Status:** fixed

### R12 — A pending Preview races Generate in the UI
- **Where:** `wails/frontend/src/main.js` (preview/generate handlers)
- **What:** Preview has no in-flight guard: a slow Preview resolving mid-run
  wipes the status line and pops its card over the progress view; resolving
  after a fast run hides the just-rendered results; double-click runs two Plans.
- **Fix:** epoch/token guard — stale async results are dropped; Preview disabled
  while a run or another preview is in flight.
- **Status:** fixed

### R13 — Emotion editor silently drops incomplete rows and collapses duplicate phrases
- **Where:** `wails/app.go` `mapFromEmotionDTO`, `wails/frontend/src/main.js` save path
- **What:** Rows with an empty phrase or tag vanish on save; duplicate phrases
  (case-insensitive) collapse last-wins; the UI reports plain "saved" (the voice
  editor, by contrast, reports skipped rows). Rows also re-sort alphabetically on
  reopen (map storage) — accepted limitation of the file format, but the silent
  loss isn't.
- **Fix:** save reports dropped/duplicate row counts like the voice editor does.
- **Status:** fixed

### R14 — "Characters to synthesize" counts bytes, not characters
- **Where:** `engine/job/job.go` (`len(text)`)
- **What:** Non-ASCII text overstates the quota-cost preview.
- **Fix:** `utf8.RuneCountInString`.
- **Status:** fixed

### R15 — A Cancel racing completion mislabels the run and can swallow a real error
- **Where:** `wails/app.go` run summary (`Canceled: ctx.Err() != nil`)
- **What:** A Cancel click landing after failures suppresses the genuine error
  and shows "stopped — run again to pick up where it left off".
- **Fix:** derive cancellation from `errors.Is(runErr, context.Canceled)` rather
  than the ctx state at summary time.
- **Status:** fixed

### R16 — Second Ctrl-C is swallowed
- **Where:** `cmd/cli/generate.go` (`signal.NotifyContext` teardown)
- **What:** SIGINT stays captured until function exit; if teardown ever stalls
  the user can't force-quit with a second Ctrl-C.
- **Fix:** release the signal handler as soon as the context is canceled so the
  next Ctrl-C takes the default action.
- **Status:** fixed

### R17 — `-default-voice` is silently ignored when `-default-speaker` is also given
- **Where:** `cmd/cli/generate.go` `applyDefaultVoice`
- **Fix:** passing both is an error.
- **Status:** fixed

### R18 — "(NaN%)" in the header when the subscription reports a zero character limit
- **Where:** `wails/frontend/src/main.js` account header
- **Fix:** guard the division.
- **Status:** fixed

### R19 — Preview samples can show files the run will skip
- **Where:** `wails/app.go` Preview (samples don't filter `UpToDate`; the
  adjacent per-voice tally does)
- **Fix:** sample from to-generate items only.
- **Status:** fixed

### R20 — Progress callbacks can be delivered out of order
- **Where:** `engine/job/job.go` worker completion → `report`
- **What:** A progress bar can go backwards; `Failed` can pair with a stale `Done`.
- **Fix:** serialize snapshot+report so reported progress is monotonic.
- **Status:** fixed

### R21 — Latent data race in lazy cleanup-rule compilation
- **Where:** `engine/text/text.go` (`Rule.compile` writes `r.re` unsynchronized)
- **What:** Unreachable today (planning is sequential) but bites the moment one
  loaded profile is used from two goroutines (e.g. app preview concurrent with a run).
- **Fix:** precompile all rules once (sync.Once) on first use.
- **Status:** fixed

### R22 — Nested/unbalanced parentheses break emotion extraction
- **Where:** `engine/emotion/emotion.go` `Extract`
- **What:** `"Hello (angry (very)) cadet"` extracts only `very` and speaks the
  literal fragment `(angry )`.
- **Fix:** iterate extraction until no parenthetical remains (bounded), so nested
  directions flatten cleanly; unbalanced parens remain visible in preview by design.
- **Status:** fixed

### R23 — Live test boxes can display stale results
- **Where:** `wails/frontend/src/main.js` (cleanup + emotion test inputs)
- **What:** One un-sequenced backend call per keystroke; an earlier slow call can
  resolve after a later one and overwrite the correct output.
- **Fix:** per-box request token; only the latest result renders.
- **Status:** fixed

### R24 — Stale comment claims PCM is wrapped into WAV
- **Where:** `engine/synth/synth.go` (`PCM_44100` comment)
- **Fix:** correct the comment (WAV wrapping is phase 2; `.pcm` is emitted today).
- **Status:** fixed

## Plumbing

### R25 — No CI on pushes; the wails module is never vetted or tested in CI
- **Where:** `.github/workflows/` (release.yml only runs on tags/dispatch; its
  verify job covers the root module only)
- **Fix:** add a CI workflow on push/PR: root module vet+test (ubuntu), wails
  module vet+build (macos).
- **Status:** fixed

### R26 — Release workflow re-downloads all Wails deps on every runner
- **Where:** `.github/workflows/release.yml` app matrix `setup-go`
- **Fix:** `cache-dependency-path: wails/go.sum`.
- **Status:** fixed

### R27 — A failed release run can't be retried
- **Where:** `.github/workflows/release.yml` (`gh release create` collides on rerun)
- **Fix:** create only if absent, upload with `--clobber`.
- **Status:** fixed

### R28 — Stale commented-out local `replace` in wails/go.mod
- **Fix:** delete it.
- **Status:** fixed

---

## Round 2 — adversarial re-review of the fixes (same day)

After the fixes above landed, a second four-way review attacked the new code
specifically and re-swept each area. The round-1 fixes held on their core
claims (close-hook fires on all platforms with no races; preview/generate
orderings all safe; lock ordering sound; containment holds). It found the
following, **all fixed the same day**:

### Defects in the round-1 fixes (regressions caught and corrected)

- **R2-1 (high)** — Legacy manifests + `-timestamps` would regenerate the whole
  batch: the `Words` flag is a bool and couldn't wildcard like the string
  fields. Now a timings request is satisfied by the sidecar existing on disk —
  which also regenerates when a sidecar was deleted, and stops entries from
  claiming sidecars that a zero-word response never wrote.
- **R2-2 (medium)** — The write-time containment check ran *after* the paid
  synthesis call, and the Babylon layout splices the **speaker** into the path
  unvalidated. Now the full layout-built path is vetted at plan time (surfaces
  in dry-run problems) and the containment check runs before any API call.
- **R2-3 (medium)** — `keys.Resolve` returned an empty key with no error for an
  explicit file containing `ELEVENLABS_API_KEY=` with no value. Now errors.
- **R2-4 (medium)** — The CLI's `-emotion` messaging was wrong in an expensive
  way: enabling emotion strips directions from **every** line (v2 voices get
  them removed, not spoken — deliberate engine behavior), so "v3 voices only"
  and especially "warning: -emotion has no effect" were false; an all-v2 user
  heeding the warning would still regenerate every parenthetical line. Help
  text, header, and the note now say what actually happens, and the model
  counts come from the actual plan, not the voices file.
- **Small (new-code) items** — stale-preview `finally` could re-enable the
  button while a newer preview was in flight; the key-save flow double-fetched
  the account and misdiagnosed network blips as "rejected key"; the dry-run
  model column was last-wins for characters sharing a voice name (now "mixed");
  `resolveModel` didn't trim pass-through IDs; test-box tokens weren't bumped
  on editor reopen. All fixed.

### New findings (missed by round 1), all fixed

- **Cleanup rule with empty `from` garbles every line** — `replace` with an
  empty match inserts the replacement between every character, passed
  validation, and would have reached a paid run. `Validate` now rejects empty
  `from` (and folds regex compilation into the race-free precompile).
- **CLI exited 0 with failed files** — scripts chaining on the exit code
  proceeded past partial failures. Failed files and dry-run problems now exit
  non-zero (documented in USAGE).
- **No macOS Edit menu** — Cmd+V couldn't paste the API key. A darwin-only
  App+Edit menu fixes clipboard shortcuts.
- **Windows close-hook freeze** — Wails invokes `OnBeforeClose` synchronously
  on the Windows message loop (macOS/Linux use a goroutine), so the
  cancel-and-wait could show "Not Responding". Windows now denies the close,
  drains off-thread, and quits programmatically.
- **No recovery from a stored invalid key** — a revoked `~/.elevenlabs_key`
  meant "account unavailable" forever with no way back to the key card short of
  deleting the dotfile. A "key…" button in the header reopens it.
- **Run summary lost on error** — Wails delivers a result *or* an error; a
  final manifest-save failure discarded the counts. Non-cancel errors now ride
  in the summary's problems list.
- **simplescript duplicate suffixing could itself collide** (`a, a, a_2` made
  two `a_2` files) — suffixing now probes until genuinely free.
- **Nested-direction whitespace** — peeling `(angry (very) shout)` left
  `"angry  shout"`, which missed the tag map; normalization now collapses runs.
- **Windows filename hazards** — IDs with `:` (NTFS stream), reserved device
  names (`NUL`, `CON.mp3`), or trailing dots/spaces are rejected at plan time.
- **Manifest temp-file collision** — two processes generating into one folder
  could interleave on the fixed `.tmp` name; saves now use a unique temp file.
- **CLI cancel labeling** — the same `ctx.Err()`-vs-`errors.Is` fix the app got.
- **Linux "Reveal" failures surfaced** — `xdg-open` failing after launch (no
  file manager) was swallowed; non-Windows now runs the opener to completion.
- **USAGE gaps** — `-default-voice` and `-key-file` added to the flags table;
  exit codes and the pre-upgrade-recast `-force` note documented.

Accepted (documented, not code-fixed): the legacy-manifest wildcard is
permanent for entries whose text never changes — a pre-upgrade voice recast is
undetectable for them (USAGE/DESIGN now say to run `-force` once in that case).

---

## Verified clean (no action)

Worker-pool bounding and cancellation (ctx flows into HTTP + backoff; no
goroutine leaks); 429/retry/backoff behavior; frontend XSS (every sink traced
through `esc()`); API-key isolation (never reaches the frontend; written 0600);
player-slot renumbering protection; cleanup/emotion ordering (directions
extracted before cleanup, tags applied only on v3); release artifact naming vs
docs vs the live v0.1.3 assets; the engine module's zero-dependency claim.
