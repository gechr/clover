# Concepts

Clover is conservative by default. It changes as little as possible, moves versions only in the safe direction, and never touches a line you did not annotate. The selection defaults below can be relaxed per annotation, per run, or in [`.clover.yaml`](configuration.md), but left alone, Clover takes the cautious path.

- **Only annotated targets change.** `clover run` rewrites the target line beside a `clover:` comment and nothing else, and `clover format` rewrites directive comments only. A file with no annotations is read, never written. A file with no comment syntax (strict JSON, a pyenv `.python-version`) is tracked by a [sidecar](sidecar.md) instead. See [Annotations](annotations.md).

- **Never downgrades.** Clover only moves a version forward. A line already ahead of the newest eligible release stays put unless you pass `downgrade` (or `--downgrade`). See [Constraints](constraints.md).

- **Stable releases only.** Prereleases (`-rc1`, `-beta`, and the like) are ignored during selection. Opt in with `prerelease` (or `--prerelease`) when you want them. See [Prereleases](prereleases.md).

- **A pinned digest never moves on its own.** A followed `sha256` or `commit` is refreshed only when the version it follows actually changes, so a re-published artifact cannot silently re-pin you. Pass `--force` (or set `run.force`) to deliberately re-pin an unchanged version. See [Checksums & digests](checksums.md).

- **Constraints cap the jump.** With a `constraint`, Clover selects the newest version *allowed*, not the newest that exists, so a `minor` bump never crosses a major. See [Constraints](constraints.md).

- **Fresh releases can wait.** A `cooldown` holds a release or a tracked digest back until it has aged, guarding against versions that are quickly yanked or patched. See [Cooldown](cooldown.md).

- **Secure pins stay in lockstep.** A commit SHA and its human-readable ref comment move together, so Clover never updates one half of the pin without the other. Credentialed commit pins are checked for impostor commits by default, `verify` tightens the check to an allowed set of branches, and Docker digest pins can require a Sigstore signer with `verify-identity`. A pin that fails verification blocks the update and fails the run. See [Verification](verification.md).

- **Shallow by default.** Clover reads only the first page of versions unless you ask for a `--deep` lookup, keeping the common run fast and within rate limits. For newest-first providers that first page holds the latest versions, and for lexically paged registries Clover warns when a deeper lookup may be needed. See [Commands](commands.md).

- **Style is preserved.** Clover keeps the line's existing shape (its `v` prefix, quoting, and surrounding text) and rewrites only the version token.

- **Deterministic and atomic.** The same inputs always produce the same output, and a write replaces the file atomically. `clover run --dry-run` resolves and renders without writing, and `clover lint` validates offline and writes nothing.

- **Fails loud.** `clover run` exits non-zero when an annotation errors, and skipped markers are reported as warnings. `clover lint` exits non-zero on errors or skips, so broken references can fail CI before a run writes anything. See [Commands](commands.md).
