# GitLab

The GitLab provider tracks the tags and releases of a project on GitLab.com or a [self-managed GitLab](#self-managed-gitlab) instance, selected with the `host` key.

```dockerfile
# clover: provider=gitlab repository=gitlab-org/cli constraint=minor
FROM registry.gitlab.com/gitlab-org/cli:v1.105.0
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `gitlab`                                                                  |
| `repository`                   | The project's full path, e.g. `group/project` or `group/subgroup/project` |
| `host`                         | The host, defaulting to `gitlab.com`                                      |
| `source`                       | What to list: `tags` (default) or `releases`                              |
| `asset`                        | Keep only releases publishing a matching asset (needs `source=releases`)  |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching tags                                                   |
| [`exclude`](filtering.md)      | Drop matching tags                                                        |
| [`prerelease`](prereleases.md) | Allow or exclude prerelease versions                                      |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

Unlike GitHub, a GitLab project path may be nested through any number of groups (`group/subgroup/project`), and Clover tracks the whole path.

## Tags and releases

The tags endpoint is queried highest-version first (`order_by=version`), so the latest version is always read on the first page, rather than the most recently updated tag, which a backport to an old release line would otherwise float to the top. Both tags and releases carry the commit they point at, which Clover records on the resolved version.

A release carries its publication date, used by [`cooldown`](cooldown.md). A tag carries its own creation date when GitLab supplies one. An annotated or lightweight tag may report none, in which case that tag is not age-checked rather than read from the target commit's possibly much older date; if no tag in the result carries a date, the marker is skipped with a warning instead of updating past a cooldown Clover cannot check. An [upcoming release](https://docs.gitlab.com/api/releases/), one scheduled for a future date, is never a candidate.

## Selecting by asset

`asset` keeps only releases whose asset links contain a name matching its glob (or `/regex/`), then selects the newest of those. It requires `source=releases`, since only releases publish assets. Note that GitLab does not publish a checksum for a release asset, so a [`value=sha256`](checksums.md) follower must source one from a checksum file rather than the asset metadata.

## CI/CD components

A [CI/CD component](https://docs.gitlab.com/ci/components/) include names its project and version on one line, so Clover recognizes it without any explicit keys. Auto-detection reads the project path from the reference (dropping the trailing component name) and tracks the project's tags:

```yaml
include:
  # @clover: constraint=minor
  - component: gitlab.com/components/opentofu/full-pipeline@2.0.1
```

A component on a self-managed instance infers its `host` from the reference as well. A reference behind a variable like `$CI_SERVER_FQDN` carries nothing to infer from, so annotate it explicitly with `provider=gitlab` and a `repository`.

## Self-managed GitLab

By default the provider targets GitLab.com. Point `host` at a self-managed instance to track a project there, and Clover routes through that instance's `/api/v4` surface instead of gitlab.com's:

```yaml
# clover: provider=gitlab host=gitlab.example.com repository=group/project constraint=minor
version: v1.4.0
```

The host is a per-marker value, so one config can track projects across GitLab.com and several self-managed instances at once.

## Authentication

Anonymous requests work but are rate-limited, and private projects need a token. Authenticate once with the device flow:

```bash
clover login gitlab                                              # GitLab.com
clover login gitlab --host gitlab.example.com --client-id <id>
```

This authorizes a read-only (`read_api`) token in the browser and stores it in your system keychain under that host. GitLab.com uses Clover's embedded OAuth application. A self-managed instance runs its own, so `--host` needs a matching `--client-id` (register an application on the instance with the device authorization grant enabled).

Alternatively, set `CLOVER_GITLAB_TOKEN` (or the ecosystem-standard `GITLAB_TOKEN`) to a [personal access token](https://docs.gitlab.com/user/profile/personal_access_tokens/) with the `read_api` scope. For safety the token is sent only to one host, `gitlab.com` by default or the host named by `CLOVER_GITLAB_HOST`, so a marker that names a different `host` never receives it. To use it against a self-managed instance, set `CLOVER_GITLAB_HOST=gitlab.example.com`.
