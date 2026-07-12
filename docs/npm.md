# npm

The npm provider tracks package versions published on the [npm registry](https://registry.npmjs.org), reading the package's packument (`registry.npmjs.org/<package>`) that lists every published version. It resolves the bare package version, e.g. a CLI tool pinned in a CI variable or an `npm install -g` line. The Node.js runtime itself is handled by the [Node.js](node.md) provider.

```yaml
# clover: provider=npm package=prettier constraint=minor
prettier_version: 3.6.2
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `npm`                                                                     |
| `package`                      | The package to track, e.g. `prettier` or `@vue/reactivity`. **Required.** |
| `registry`                     | An npm-compatible registry base URL, default `https://registry.npmjs.org` |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The registry is public for reads, so the npm provider needs no authentication. It is selected explicitly with `provider=npm`; a bare version line carries no signal to infer it from.

## Scoped packages

A scoped name is written as published, `@scope/name`:

```yaml
# clover: provider=npm package=@vue/reactivity constraint=minor
reactivity_version: 3.5.39
```

The packument serves the package's whole version history in one response, so Clover always sees every version, and `--deep` has nothing extra to fetch. The packument also dates every version, so a [`cooldown`](cooldown.md) can hold a fresh release back. Prerelease versions (`3.6.0-beta.17`) are published alongside the stables and stay excluded unless [allowed](prereleases.md).

## Custom registries

The `registry` key points discovery at any npm-compatible registry, e.g. a corporate mirror. The value is the registry base URL (http or https, a trailing slash is tolerated), and the packument is fetched from `<registry>/<package>` exactly as on the public registry:

```yaml
# clover: provider=npm package=left-pad registry=https://npm.internal.corp constraint=minor
leftpad_version: 1.3.0
```

Access is anonymous. A registry that requires authentication for reads is not supported.

## Checksums

The registry's own integrity hashes are sha1 and sha512, which Clover does not consume, and npm publishes no sha256 checksum file. A [follower](checksums.md) can still keep a sha256 in lockstep with the version: each candidate carries its tarball as an asset, so [`value=sha256`](checksums.md#sourcing-a-sha256) downloads the tarball and hashes it. Select it with `pattern` - the tarball name is the package's unscoped basename, `<name>-<version>.tgz` (`reactivity-3.5.39.tgz` for `@vue/reactivity`):

```yaml
# clover: provider=npm package=prettier id=prettier constraint=minor
prettier_version: 3.6.2

# clover: from=prettier value=sha256 pattern=prettier-<version>.tgz
prettier_sha256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step, and a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
