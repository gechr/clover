# Helm

The Helm provider tracks chart versions from both classic HTTP chart repositories (an `index.yaml` served under the repo URL) and OCI registries (`oci://`, where a chart's versions are the repository's tags).

```yaml
# clover: provider=helm registry=https://charts.bitnami.com/bitnami chart=nginx constraint=minor
version: 18.2.0
```

## Keys

| Key                            | Description                                                                                                                |
| ------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| `provider`                     | `helm`                                                                                                                     |
| `registry`                     | The chart source. An `https://` (or `http://`) URL is a classic repository, while an `oci://` URL is an OCI registry base. |
| `chart`                        | The chart name (e.g. `nginx`). A bare name, since the repository path belongs in `registry`.                               |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range)                                                  |
| [`include`](filtering.md)      | Keep only matching versions                                                                                                |
| [`exclude`](filtering.md)      | Drop matching versions                                                                                                     |
| [`prerelease`](prereleases.md) | Allow or exclude prerelease versions                                                                                       |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible (classic repositories only, see below)                                  |

The provider is selected explicitly with `provider=helm`, or [inferred](auto.md) from a `dependencies` entry in a `Chart.yaml`, where the entry's `name` supplies the chart and its `repository` the registry.

## Classic vs OCI

The `registry` scheme selects how Clover looks up versions:

- **Classic (`https://`).** Clover fetches `<registry>/index.yaml` and reads the named chart's published versions. The index carries each version's release date, so [`cooldown`](cooldown.md) applies, and the chart-tarball checksum, which a [follower](checksums.md) can source without a download.
- **OCI (`oci://`).** Clover lists the chart's tags from the registry. OCI tags carry no dates, so `cooldown` does not apply. Pass [`--deep`](commands.md) if a chart has more tags than fit on one page.

```yaml
# clover: provider=helm registry=oci://registry-1.docker.io/bitnamicharts chart=nginx constraint=minor
version: 18.2.0
```

## Checksums and digests

A classic repository's index publishes the chart-tarball checksum, so a [follower](checksums.md) can render it alongside the version:

```yaml
# clover: provider=helm registry=https://charts.bitnami.com/bitnami chart=nginx id=chart constraint=minor
version: 18.2.0
# clover: from=chart value=sha256 pattern=*.tgz
digest: 0000000000000000000000000000000000000000000000000000000000000000
```

For an `oci://` chart, Clover resolves the manifest digest, so a [`value=sha256`](checksums.md) pin stays fresh.

## Authentication

Classic repositories are usually public. For a private OCI chart registry, run `helm registry login` (or `docker login`), or set `CLOVER_HELM_TOKEN`, and Clover reuses those credentials.
