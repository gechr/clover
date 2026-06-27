# GitHub

The GitHub provider tracks the releases and tags of a repository.

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

## Keys

| Key                                          | Description                                                                |
| -------------------------------------------- | -------------------------------------------------------------------------- |
| `provider`                                   | `github`.                                                                  |
| `repository`                                 | The `owner/name` of the repository to track.                               |
| `source`                                     | What to list: `tags` (default) or `releases` (required for `asset`).       |
| [`constraint`](constraints.md)               | How far the version may move (`major`/`minor`/`patch`, or a semver range). |
| [`include` / `exclude`](filtering.md)        | Filter the candidate tags.                                                 |
| `asset`                                      | Keep only releases publishing an asset whose filename matches (see below). |
| [`prerelease`](prereleases.md)               | Allow or exclude prerelease versions.                                      |
| [`cooldown`](cooldown.md)                    | Require a minimum age before a release is eligible.                        |
| [`track`](tracking.md)                       | Track a branch HEAD instead of selecting a version.                        |
| [`verify`, `verify-branch`](verification.md) | Deep-verify a secure pin against upstream.                                 |

## Selecting by asset

Some releases matter only when they ship a particular artifact - a Linux binary, say. `asset` keeps only releases whose asset list contains a filename matching its glob (or `/regex/`), then selects the newest of those. It requires `source=releases`, since only releases publish assets.

```yaml
# clover: provider=github repository=owner/tool source=releases asset=*linux_amd64.tar.gz constraint=minor
version: v1.4.0
```

Pair it with a [`value=sha256`](checksums.md) follower to source that asset's checksum alongside the version. `asset` selects which release; [`pattern`](checksums.md) selects which of its assets to hash.

## Authentication

Anonymous requests work but are rate-limited, and private repositories need a token. Authenticate once with the device flow:

```bash
clover login
```

## Pinning an Action to a commit

GitHub Actions are pinned to a full commit SHA, with the human-readable ref kept in a trailing comment. Clover keeps both halves of the pin in step - the commit SHA *and* the comment - so the comment can never drift away from the commit it names. A secure pin is recognized by its shape (`uses: owner/repo@<40-hex-sha>`), not its location, so a pin is kept fresh wherever it lives: a workflow, a composite `action.yml`, or a reusable-workflow caller.

A pin whose comment is a **version tag** tracks releases. Clover resolves the newest tag allowed by the directive's [`constraint`](constraints.md), then rewrites the SHA to that tag's commit and the comment to the tag - together, in one pass:

```yaml
# clover: provider=github repository=actions/checkout constraint=major
- uses: actions/checkout@8f4b7f84864484a7bf31766abe9204da3cbe65b3 # v3.5.0
```

A pin whose comment is a **branch name** tracks that branch's HEAD instead of selecting a version: the comment stays put while the SHA is re-resolved each run. See [Tracking](tracking.md) and [Verification](verification.md):

```yaml
# clover: provider=github track=main verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

A tag-pinned `uses:` with no SHA (`@v4`) carries no paired commit, so Clover simply bumps the ref in place. To project a resolved commit onto a separate target line, use [`value=commit`](checksums.md).
