# Rust

The Rust provider tracks the toolchain releases published at [`static.rust-lang.org`](https://static.rust-lang.org/), reading the manifest index (`static.rust-lang.org/manifests.txt`) that lists every channel manifest ever published. It resolves the bare toolchain version, such as a `RUST_VERSION` CI variable, a mise `rust` pin, a pinned `channel` in `rust-toolchain.toml`, or a `rust-version` floor in `Cargo.toml`. A `rust:1.97-slim` container tag is a Docker image, handled by the [Docker](docker.md) provider.

```yaml
# clover: provider=rust constraint=minor
RUST_VERSION: 1.97.0
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `rust`                                                                    |
| `channel`                      | Release channel to track: `stable` (the default) or `beta`                |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The manifest index is public, so the Rust provider needs no authentication. It is selected explicitly with `provider=rust`, or [inferred](auto.md) from a `rust` pin in a mise configuration or `.tool-versions` file, a version-pinned `channel` in `rust-toolchain.toml`, or a `rust-version` floor in `Cargo.toml`. Like a `requires-python` floor, `rust-version` is bumped in place with its precision preserved, so `"1.70"` advances only when a new minor line ships.

Each release is dated by the directory its manifest was published under, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole release history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch. The index starts at Rust 1.8.0 and its numbered beta snapshots at 1.75.0, so earlier releases predate it and cannot be resolved.

## Channels

Rust ships on three [release channels](https://forge.rust-lang.org/#current-release-versions). By default Clover tracks **stable**, so the newest release satisfying the `constraint` wins. `channel=beta` lists the numbered **beta** snapshots (`1.98.0-beta.1`) instead:

```yaml
# clover: provider=rust channel=beta
RUST_VERSION: 1.98.0-beta.1
```

A beta version's dash-suffix behaves like any other suffix on a line: selection stays on the numbered track already written there, so `1.98.0-beta.1` advances to the next cycle's first snapshot (`1.99.0-beta.1`) as soon as it ships. Since every cycle publishes a `beta.1`, that is the number to pin. Moving *across* numbers instead, so the line always holds the newest snapshot whatever its number, takes an explicit [`include`](filtering.md) to unpin the track and [`prerelease=true`](prereleases.md) because betas then face the prerelease gate:

```yaml
# clover: provider=rust channel=beta include=*-beta.* prerelease=true
RUST_VERSION: 1.98.0-beta.1
```

The **nightly** channel is not trackable: a nightly build is a dated snapshot (`nightly-2026-07-11`) with no version of its own, so there is nothing version-shaped to resolve, and `channel=nightly` is rejected.

## Checksums

Rust publishes a `.sha256` file beside every release artifact at a predictable URL, so a [follower](checksums.md) can keep a checksum in lockstep with the version by templating [`sha256-url`](checksums.md#sourcing-a-sha256) with `<version>` and selecting the artifact with `pattern`:

```yaml
# clover: provider=rust id=rust constraint=minor
RUST_VERSION: 1.97.0

# clover: from=rust value=sha256 sha256-url=https://static.rust-lang.org/dist/rust-<version>-x86_64-unknown-linux-gnu.tar.xz.sha256 pattern=rust-<version>-x86_64-unknown-linux-gnu.tar.xz
RUST_SHA256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step, and a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
