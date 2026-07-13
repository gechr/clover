# Swift

The Swift provider tracks the Swift toolchain releases published at [`swift.org`](https://www.swift.org/install/), reading the release index (`www.swift.org/api/v1/install/releases.json`) that lists every release. It resolves the bare toolchain version, such as a `SWIFT_VERSION` CI variable, a mise `swift` pin, or a `.swift-version` file.

```yaml
# clover: provider=swift constraint=minor
SWIFT_VERSION: 6.3.3
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `swift`                                                                   |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The release index is public, so the Swift provider needs no authentication. It is selected explicitly with `provider=swift`, or [inferred](auto.md) from a `swift` pin in a mise configuration, a `.tool-versions` file, or a `.swift-version` file.

The index names each release by its bare version (`6.3.3`, or a two-component `5.10` on older lines), matching a bare on-line reference, while the release tag (`swift-6.3.3-RELEASE`) stays upstream and resolves the reported link. Each release carries its publication date, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole release history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch. Snapshot toolchains are moving development builds that never appear in the release index, so only shipped releases are tracked.

## Checksums

`swift.org` embeds a SHA256 checksum for each release's Static, Wasm, and Android SDK artifacts in the same index, so a [follower](checksums.md) sources one for free with no extra request. Select the artifact by its stable platform key:

```yaml
# clover: provider=swift id=swift constraint=minor
SWIFT_VERSION: 6.3.3

# clover: from=swift value=sha256 pattern=static-sdk
SWIFT_STATIC_SDK_SHA256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
