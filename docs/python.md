# Python

The Python provider tracks the CPython releases published at [`python.org`](https://www.python.org/downloads/), reading the downloads API (`python.org/api/v2/downloads/release/`) that lists every release. It resolves the bare interpreter version, such as a `PYTHON_VERSION` CI variable, a mise `python` pin, or a ruff/black `target-version` in `pyproject.toml`. A `python:3.14-slim` container tag is a Docker image, handled by the [Docker](docker.md) provider.

```yaml
# clover: provider=python constraint=minor
PYTHON_VERSION: 3.14.6
```

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `python`                                                                  |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The downloads API is public, so the Python provider needs no authentication. It is selected explicitly with `provider=python`, or [inferred](auto.md) from a `target-version` in `pyproject.toml` or a `python` pin in a mise configuration. A `requires-python` floor is deliberately not inferred, since it declares a minimum supported version rather than the interpreter to track.

Each release name carries a `Python` prefix (`Python 3.14.6`). Clover parses out the version so the resolved value is clean semver (`3.14.6`), matching a bare on-line reference. The API carries each release's publication date, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole release history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch.

## Prereleases

Python alphas, betas, and release candidates are published with a dashless suffix (`3.15.0b3`, `3.14.0rc1`). Clover normalizes these to canonical semver (`3.15.0-b3`) and treats them as [prereleases](prereleases.md), excluded by default and selected only when prereleases are allowed:

```yaml
# clover: provider=python prerelease=true
PYTHON_VERSION: 3.15.0-b3
```

## Compact target versions

Ruff, Black, and mypy write a compact target in `pyproject.toml`, `py` followed by the major and minor with no dot (`py314` for Python 3.14). Clover recognizes this form, tracks the corresponding minor line, and renders the result back into the same compact form:

<!-- clover-lint-skip -->

```toml
# clover: provider=python
target-version = "py314"
```

A bump to Python 3.15 rewrites it to `py315`. A patch release within the same minor line leaves it unchanged, since the compact form carries only the minor. An array of targets (`["py311", "py312"]`) is left alone, since which one to track is ambiguous.

Checksums are not sourced for Python releases: the downloads API lists releases without per-file digests, so there is no digest to follow.
