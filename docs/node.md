# Node.js

The Node.js provider tracks the runtime versions published at [`nodejs.org`](https://nodejs.org/dist/), reading the release index (`nodejs.org/dist/index.json`) that lists every release. It resolves the bare runtime version (e.g. a `.node-version` file or a CI variable) - a `node:24-bookworm` container tag is a Docker image, handled by the [Docker](docker.md) provider.

```yaml
# clover: provider=node constraint=minor
node_version: 24.18.0
```

## Keys

| Key                                   | Description                                                                      |
| ------------------------------------- | -------------------------------------------------------------------------------- |
| `provider`                            | `node`.                                                                          |
| `lts`                                 | Restrict candidates to the long-term-support release lines. Defaults to `false`. |
| [`constraint`](constraints.md)        | How far the version may move (`major`/`minor`/`patch`, or a semver range).       |
| [`include` / `exclude`](filtering.md) | Filter the candidate versions.                                                   |
| [`cooldown`](cooldown.md)             | Require a minimum age before a version is eligible.                              |

The release index is public, so the Node.js provider needs no authentication. It is selected explicitly with `provider=node` - a bare version line carries no signal to [infer](auto.md) it from.

## Release lines

Node.js ships a new **current** major line every six months; the even-numbered lines become **long-term support (LTS)** and are maintained for years, each under a codename (`Krypton`, `Jod`, `Iron`, `Hydrogen`, ...). By default Clover tracks every release, so the newest version satisfying the `constraint` wins:

```yaml
# clover: provider=node constraint=minor
node_version: 24.18.0
```

`lts=true` keeps only the LTS lines, dropping the current (odd or pre-promotion) releases - the form to track when you want stability over the newest features:

```yaml
# clover: provider=node lts=true constraint=major
node_version: 24.18.0
```

The index serves the whole release history in one response, so Clover always sees every line - `--deep` has nothing extra to fetch.

## Checksums

Node.js publishes a `SHASUMS256.txt` file for every release at a predictable URL, so a [follower](checksums.md) can keep a checksum in lockstep with the version by templating [`sha256-url`](checksums.md#sourcing-a-sha256) with `<version>` and selecting the artifact with `pattern`:

```yaml
# clover: provider=node id=node constraint=minor
node_version: 24.18.0

# clover: from=node value=sha256 sha256-url=https://nodejs.org/dist/v<version>/SHASUMS256.txt pattern=node-<version>-linux-x64.tar.xz
node_sha256: 0000000000000000000000000000000000000000000000000000000000000000
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step - a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
