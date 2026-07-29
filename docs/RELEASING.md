# Cutting a release

```bash
git add -A
git commit -m "what changed [release]"

git push                      # sync main (ci skips itself — see below)

git tag -a v0.1.7 -m "Nth release: what changed"
git push origin v0.1.7        # triggers the release build
```

Notes:

- **`[release]` in the commit message** makes the `ci` workflow skip its jobs —
  the tag push runs `release.yml` moments later, whose verify job covers the
  same ground. The ci entry still appears in the Actions list but as a gray
  0-second "skipped" row, not a second build. Don't use GitHub's `[skip ci]`
  instead: it also suppresses the tag-triggered release workflow.
- **Don't skip the plain `git push`.** Pushing only the tag leaves `main` on
  GitHub behind (the code browser/clones miss the release commit) and the
  Actions run gets the generic title "release" instead of the commit message —
  that's what happened to v0.1.5.
- Tags are annotated (`-a -m`), matching v0.1.0 onward; the message convention
  is "Nth release: summary".
- Re-running a failed tag build is safe: the release job creates the GitHub
  Release only if absent and uploads assets with `--clobber`.
