# Gitea

The Gitea provider tracks the tags and releases of a repository on a Gitea or Forgejo forge. One provider serves every instance of the API - [Codeberg](https://codeberg.org) (the default), [forgejo.org](https://forgejo.org), and any self-managed Gitea/Forgejo - selected with the `host` key.

```dockerfile
# clover: provider=gitea repository=forgejo/forgejo constraint=minor
FROM codeberg.org/forgejo/forgejo:15.0.3
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `gitea`                                                                   |
| `repository`                   | The repository as `owner/name`                                            |
| `host`                         | The forge host; defaults to `codeberg.org`                                |
| `source`                       | What to list: `tags` (default) or `releases`                              |
| `asset`                        | Keep only releases publishing a matching asset (needs `source=releases`)  |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching tags                                                   |
| [`exclude`](filtering.md)      | Drop matching tags                                                        |
| [`prerelease`](prereleases.md) | Allow or exclude prerelease versions                                      |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

A repository is a flat `owner/name`: Gitea and Forgejo organize repositories under a single owner, with no nested subgroups. Point `host` at a private instance to track it the same way - `host=git.example.com`.

## Tags and releases

Gitea orders tags by creation date and offers no version-sort, so the newest tag - not necessarily the highest version - heads the listing. A shallow lookup reads the first page; when more tags remain, Clover suggests `--deep` to read them all. A tag records the commit SHA it points at; a release does not (Gitea's release target may be empty or a branch name, not a SHA), so a `value=commit` follower works with `source=tags`.

A release carries its publication date, used by [`cooldown`](cooldown.md), and an upstream `prerelease` flag, which [prerelease](prereleases.md) filtering honors even when the tag itself looks stable. A tag carries no date of its own on Gitea - only its target commit's - so rather than age a tag by a commit that may long predate it, `cooldown` is simply inert for a tag. A [draft release](https://forgejo.org/docs/latest/user/release/) is unpublished and never a candidate.

## Selecting by asset

`asset` keeps only releases whose attachments contain a name matching its glob (or `/regex/`), then selects the newest of those. It requires `source=releases`, since only releases publish assets. Gitea does not publish a checksum alongside a release asset, so a [`value=sha256`](checksums.md) follower must source one from a checksum file rather than asset metadata.

## Authentication

Anonymous requests work but are rate-limited, and private repositories need a token. Authenticate once with a browser login:

```bash
clover login gitea                       # Codeberg
clover login gitea --host git.example.com
```

This opens the host's authorization page, captures the result on a local loopback port, and stores a read-only token in your system keychain under that host (refreshed automatically as it expires). It uses Gitea's built-in `tea` OAuth application, so no app registration is needed on a stock instance; pass `--client-id` if the instance disabled its built-in applications. Because it drives a local browser and loopback, run it on your own machine - over SSH, use a token instead.

Alternatively, set `CLOVER_GITEA_TOKEN` (or the ecosystem-standard `GITEA_TOKEN`) to a [personal access token](https://docs.gitea.com/development/api-usage#authentication) with read access. The environment variable takes precedence, suiting CI and headless runs. For safety it is sent only to one host - `codeberg.org` by default, or the host named by `CLOVER_GITEA_HOST` - so a marker that names a different `host` never receives the token. To use a token against a self-managed instance, set `CLOVER_GITEA_HOST=git.example.com`.
