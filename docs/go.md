# Go

The Go provider tracks the Go toolchain releases published at [`go.dev`](https://go.dev/dl/), reading the download index (`go.dev/dl/?mode=json&include=all`) that lists the full release history. It resolves the bare toolchain version, such as a `go.mod` `go` directive, a `GO_VERSION` CI variable, or a mise `go` pin. A `golang:1.26-bookworm` container tag is a Docker image, handled by the [Docker](docker.md) provider.

```yaml
# clover: provider=go constraint=minor
GO_VERSION: 1.26.5
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `go`                                                                      |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |

The download index is public, so the Go provider needs no authentication. It is selected explicitly with `provider=go`, or [inferred](auto.md) from a `go` or `toolchain` directive in a `go.mod` or `go.work` file or a `go` pin in a mise configuration.

Every `go.dev` version carries a `go` prefix (`go1.26.5`). Clover strips it so the resolved value is clean semver (`1.26.5`), which matches a bare on-line reference and renders cleanly through [`<version>`](find-replace.md). The index serves the whole release history in one response, so Clover always sees every release and `--deep` has nothing extra to fetch. `go.dev` publishes no per-release dates, so [`cooldown`](cooldown.md) is unsupported here: setting it holds the line with a `cooldown not supported` warning rather than bumping past a cooldown that cannot be measured.

## Prereleases

Go release candidates and betas are published with a dashless suffix (`go1.27rc1`, `go1.27beta1`). Clover normalizes these to canonical semver (`1.27.0-rc1`) and treats them as [prereleases](prereleases.md), so they are excluded by default and only selected when prereleases are allowed:

```yaml
# clover: provider=go prerelease=true
GO_VERSION: 1.27.0-rc1
```

On the line, Clover keeps the spelling the pin already uses, so a dashless pin like `toolchain go1.27rc1` bumps to `go1.27rc2` rather than the dashed form. A line moving from a stable version to its first prerelease is written in the canonical dashed form, which a `go.mod` `toolchain` directive and `GOTOOLCHAIN` do not accept, so spell the first prerelease pin dashless by hand.

## Keeping the `go` prefix

When the target line keeps the `go` prefix, as a `GOTOOLCHAIN` directive requires, anchor a [`find`](find-replace.md) pattern on it: the `<version>` token substitutes the resolved version in place while the literal `go` is preserved.

```yaml
# clover: provider=go find=go<version>
GOTOOLCHAIN: go1.26.5
```

A `go.mod` `toolchain` directive carries the same prefix but needs no `find`: Clover recognizes the directive and anchors on the literal `go` automatically.

<!-- clover-lint-skip -->

```text
// clover: provider=go constraint=minor
toolchain go1.26.5
```

## Checksums

`go.dev` embeds each archive's SHA256 in the same download index, so a [follower](checksums.md) sources it for free with no extra request. Select the archive with `pattern`, templating [`<version>`](find-replace.md) to track the resolved version:

```yaml
# clover: provider=go id=go constraint=minor
GO_VERSION: 1.26.5

# clover: from=go value=sha256 pattern=go<version>.linux-amd64.tar.gz
GO_SHA256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
