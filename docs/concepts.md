# Concepts

Clover is conservative by default. It changes as little as possible, moves versions only in the safe direction, and never touches a line you did not annotate. Every default below can be relaxed when you ask for it - per annotation, per run, or in [`.clover.yaml`](configuration.md) - but left alone, Clover always takes the cautious path.

- **Only annotated lines change.** Clover rewrites the line beside a `clover:` comment and nothing else; a file with no annotations is read, never written. See [Annotations](annotations.md).

- **Never downgrades.** Clover only moves a version forward. A line already ahead of the newest eligible release stays put unless you pass `downgrade` (or `--downgrade`). See [Constraints](constraints.md).

- **Stable releases only.** Prereleases (`-rc1`, `-beta`, and the like) are ignored during selection; opt in with `prerelease` (or `--prerelease`) when you want them. See [Prereleases](prereleases.md).

- **A pinned digest never moves on its own.** A followed `sha256` or `commit` is refreshed only when the version it follows actually changes, so a re-published artifact cannot silently re-pin you. Pass `--force` (or set `run.force`) to deliberately re-pin an unchanged version. See [Checksums & digests](checksums.md).

- **Constraints cap the jump.** With a `constraint`, Clover selects the newest version *allowed*, not the newest that exists - so a `minor` bump never crosses a major. See [Constraints](constraints.md).

- **Fresh releases can wait.** A `cooldown` holds a release - or a tracked digest - back until it has aged, guarding against versions that are quickly yanked or patched. See [Cooldown](cooldown.md).

- **Secure pins stay in lockstep.** A commit SHA and its human-readable ref comment always move together, and `verify` fails closed: a pin that cannot be confirmed against its branch is rejected, not written. See [Verification](verification.md).

- **Shallow by default.** Clover reads only the newest page of versions unless you ask for a `--deep` lookup, keeping the common run fast and within rate limits. See [Commands](commands.md).

- **Style is preserved.** Clover keeps the line's existing shape - its `v` prefix, quoting, and surrounding text - and rewrites only the version token.

- **Deterministic and atomic.** The same inputs always produce the same output, and a write replaces the file atomically. `clover run --dry-run` and `clover lint` resolve everything but write nothing.

- **Fails loud.** `clover run` and `clover lint` exit non-zero when any annotation cannot be resolved, so a broken reference fails CI instead of passing silently. See [Commands](commands.md).
