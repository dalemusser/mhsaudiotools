# Pronunciation dictionaries — live verification (2026-07-27)

Verified against the project account by live synthesis, the same way the v2/v3
voice support was settled. Method: a temporary dictionary with two rules —
an **alias** mapping `WAT247` to a deliberately long phrase, and a **phoneme**
rule mapping the nonsense word `blip` to a very long IPA pronunciation
(supercalifragilisticexpialidocious) — then the same texts synthesized with and
without the dictionary via `/with-timestamps`. If a rule applies, the clip
duration jumps unmistakably; the alignment shows what text the captions would
carry.

## Results

| Test | Model | Without dict | With dict | Verdict |
|---|---|---|---|---|
| Alias | `eleven_multilingual_v2` | 2.09s | **4.69s** | **applies** |
| Alias | `eleven_v3` | 2.72s | **6.88s** | **applies** |
| Phoneme | `eleven_multilingual_v2` | 1.39s | 1.30s | silently ignored |
| Phoneme | `eleven_v3` | 1.76s | 0.83s | silently ignored |
| Phoneme (control) | `eleven_turbo_v2` | 1.49s | **3.34s** | applies — rule is valid |
| Phoneme (control) | `eleven_flash_v2` | 1.49s | **3.48s** | applies — rule is valid |

Additional facts established:

- **Alignment keeps the original spelling.** With the alias applied, the
  character alignment still reads `Welcome to WAT247 cadet.` — so word timings
  and Babylon captions would show the writers' text while the audio speaks the
  expansion. This is the capability our client-side cleanup replacement cannot
  provide (it changes the text before sending, so captions show "Watt 2 4 7").
- **Phoneme rules are a silent no-op on our models.** The API accepts the rule
  and the locator without error on multilingual v2 and v3; it simply has no
  effect. The control runs prove the rule itself is well-formed.
- The dictionary API works as documented: `add-from-rules` returns
  `{id, version_id}`; TTS takes `pronunciation_dictionary_locators` (≤3).
  There is **no DELETE endpoint** (405) — dictionaries can only be added
  to/versioned, so test dictionaries linger in the account (harmless unless
  referenced). A `mhs-verify-temp` dictionary from this verification remains.

## Implication for the app

- The only rule type our models honor (**alias**) duplicates what cleanup
  profiles already do — except server-side, with original-spelling captions.
- **Phoneme precision — the feature that would justify building dictionary
  management into the app — is not available on the models we render with.**

Decision (superseded same day): **built** — text highlighting during dialog
made display-text-accurate timings a requirement, which only the dictionary
provides. Implementation: `engine/pron` (rules file + publish state), alias
locators on every request, rules migrated out of the cleanup profile, a
pronunciations editor in the app, `-pronunciations`/`-no-pronunciations` in the
CLI, auto-(re)publish at run start (create once, then diff-update the same
dictionary in place — add-rules/remove-rules were also verified live, since
dictionaries cannot be deleted), and a per-line manifest key so editing a rule
regenerates exactly the lines containing its word. Phoneme rules remain
unavailable on our models; the engine sends alias rules only.
