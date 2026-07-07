# GitHub

The GitHub provider tracks the releases and tags of a repository on GitHub.com or a [GitHub Enterprise Server](#github-enterprise-server) instance, selected with the `host` key.

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

## Keys

| Key                                          | Description                                                               |
| -------------------------------------------- | ------------------------------------------------------------------------- |
| `provider`                                   | `github`                                                                  |
| `repository`                                 | The `owner/name` of the repository to track                               |
| `host`                                       | The host, defaulting to `github.com`                                      |
| `source`                                     | What to list: `tags` (default) or `releases` (required for `asset`)       |
| [`constraint`](constraints.md)               | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)                    | Keep only matching tags                                                   |
| [`exclude`](filtering.md)                    | Drop matching tags                                                        |
| `asset`                                      | Keep only releases publishing an asset whose filename matches (see below) |
| [`prerelease`](prereleases.md)               | Allow or exclude prerelease versions                                      |
| [`cooldown`](cooldown.md)                    | Require a minimum age before a release is eligible                        |
| [`track`](tracking.md)                       | Track a branch HEAD instead of selecting a version                        |
| [`verify`, `verify-branch`](verification.md) | Deep-verify a secure pin against upstream                                 |

## Tags and releases

`source` chooses what to list. `tags` (the default) reads the repository's tags; `releases` reads its published releases. The difference matters for [`cooldown`](cooldown.md): a GitHub tag carries no publication date of its own, only its target commit's, so Clover cannot age a tag without guessing from a commit that may long predate it. Rather than silently update past a cooldown it cannot check, a `cooldown` on `source=tags` **skips the marker with a warning** and holds the line. A release carries its own `published_at`, so set `source=releases` to have cooldown apply.

## Selecting by asset

Some releases matter only when they ship a particular artifact, such as a Linux binary. `asset` keeps only releases whose asset list contains a filename matching its glob (or `/regex/`), then selects the newest of those. It requires `source=releases`, since only releases publish assets.

```yaml
# clover: provider=github repository=owner/tool source=releases asset=*linux_amd64.tar.gz constraint=minor
version: v1.4.0
```

Pair it with a [`value=sha256`](checksums.md) follower to source that asset's checksum alongside the version. `asset` selects which release, and [`pattern`](checksums.md) selects which of its assets to hash.

## GitHub Enterprise Server

By default the provider targets GitHub.com. Point `host` at a GitHub Enterprise Server instance to track a repository there, and Clover routes through that instance's API (`/api/v3` and `/api/graphql`) instead of `api.github.com`:

```yaml
# clover: provider=github host=ghe.example.com repository=org/tool constraint=minor
version: v1.4.0
```

The host is a per-marker value, so one config can track repositories across GitHub.com and several enterprise instances at once.

## Authentication

Anonymous requests work but are rate-limited, and private repositories need a token. Authenticate once with the device flow:

```bash
clover login
clover login github --host ghe.example.com --client-id <id>
```

GitHub.com uses Clover's embedded OAuth app. A GitHub Enterprise Server instance runs its own, so `--host` needs a matching `--client-id` (register an OAuth app on the instance with the device flow enabled). The minted token is stored in your system keychain under that host.

Alternatively, set `CLOVER_GITHUB_TOKEN` to a [personal access token](https://docs.github.com/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens) with read access. Clover also honors a `gh`-compatible token (`GH_TOKEN`/`GITHUB_TOKEN`, or `GH_ENTERPRISE_TOKEN`/`GITHUB_ENTERPRISE_TOKEN` for an enterprise host). For safety `CLOVER_GITHUB_TOKEN` is sent only to one host, `github.com` by default or the host named by `CLOVER_GITHUB_HOST`, so a marker that names a different `host` never receives it. To use it against an enterprise instance, set `CLOVER_GITHUB_HOST=ghe.example.com`.

## Pinning an Action to a commit

GitHub Actions are pinned to a full commit SHA, with the human-readable ref kept in a trailing comment. Clover keeps both halves of the pin in step, the commit SHA *and* the comment, so the comment can never drift away from the commit it names. A secure pin is recognized by its shape (`uses: owner/repo@<40-hex-sha>`), not its location, so a pin is kept fresh wherever it lives: a workflow, a composite `action.yml`, or a reusable-workflow caller.

A pin whose comment is a **version tag** tracks releases. Clover resolves the newest tag allowed by the directive's [`constraint`](constraints.md), then rewrites the SHA to that tag's commit and the comment to the tag, together in one pass:

```yaml
# clover: provider=github repository=actions/checkout constraint=major
- uses: actions/checkout@8f4b7f84864484a7bf31766abe9204da3cbe65b3 # v3.5.0
```

A pin with **no comment at all** is documented on the next `run`. With no comment to anchor a relative `constraint`, Clover resolves the newest version (or one a range allows), rewrites the SHA, and appends the comment, so `- uses: actions/checkout@<sha>` becomes `- uses: actions/checkout@<sha> # v4.2.0`. A bare pin becomes a self-documenting one.

A pin whose comment is a **branch name** tracks that branch's HEAD instead of selecting a version. The comment stays put while the SHA is re-resolved each run. See [Tracking](tracking.md) and [Verification](verification.md):

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=main verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

A tag-pinned `uses:` with no SHA (`@v4`) is converted to the secure pin format on the next `run`. Clover is secure by default, so the tag is replaced by the resolved version's full commit SHA and the version itself lands in the trailing comment, at its full precision regardless of how the original tag was written. `- uses: actions/checkout@v4` becomes `- uses: actions/checkout@<sha> # v4.2.2`, and from then on the line is a secure pin kept fresh as above. To project a resolved commit onto a separate target line, use [`value=commit`](checksums.md).
