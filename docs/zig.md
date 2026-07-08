# Zig

The Zig provider tracks the Zig toolchain releases published at [`ziglang.org`](https://ziglang.org/download/), reading the download index (`ziglang.org/download/index.json`) that lists every release. It resolves the bare toolchain version, such as a `ZIG_VERSION` CI variable or a mise `zig` pin.

```yaml
# clover: provider=zig constraint=minor
ZIG_VERSION: 0.15.2
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `zig`                                                                     |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The download index is public, so the Zig provider needs no authentication. It is selected explicitly with `provider=zig`, or [inferred](auto.md) from a `zig` pin in a mise configuration.

The index keys the releases by version, which is already clean semver (`0.15.2`), matching a bare on-line reference. Each release carries its publication date, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole release history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch. The `master` nightly build is a moving pointer rather than a release, so it is never tracked, and a `build.zig.zon` `minimum_zig_version` is a floor, not the version to track, so it is left alone.

## Checksums

`ziglang.org` embeds each archive's SHA256 in the same download index, so a [follower](checksums.md) sources it for free with no extra request. Select the archive by its platform key, which stays stable even though the tarball filename drifts across releases:

```yaml
# clover: provider=zig id=zig constraint=minor
ZIG_VERSION: 0.15.2

# clover: from=zig value=sha256 pattern=x86_64-linux
ZIG_SHA256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
