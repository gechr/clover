# PyPI

The PyPI provider tracks the versions of a Python package published on [`pypi.org`](https://pypi.org), reading the JSON API (`pypi.org/pypi/<package>/json`) that lists a package's whole release history. It resolves the version inside a dependency specifier, e.g. a `pyproject.toml` dependency entry or a pinned requirement.

```toml
[build-system]
# clover: provider=pypi package=uv_build constraint=minor
requires = ["uv_build>=0.8.24"]
```

In a `pyproject.toml` file the directive can be a bare [`@clover`](auto.md#the-clover-shorthand): the package name is [inferred](auto.md) from the specifier itself.

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `pypi`                                                                    |
| `package`                      | The package name, as spelled on the line or on PyPI. **Required.**        |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The JSON API is public, so the PyPI provider needs no authentication. It is selected explicitly with `provider=pypi package=<name>`, or [inferred](auto.md) from a quoted dependency specifier in `pyproject.toml`, which also supplies the `package`. The name is normalized the way PyPI itself does, so `uv_build`, `uv-build`, and `UV.Build` all reach the same project.

Each file upload carries a timestamp, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole release history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch.

## Prereleases

PyPI publishes prereleases with a dashless [PEP 440](https://peps.python.org/pep-0440/) suffix (`0.5.30rc1`, `2.0.0b1`). Clover normalizes these to canonical semver (`0.5.30-rc1`) and treats them as [prereleases](prereleases.md), excluded by default and selected only when prereleases are allowed. Versions outside the semver shape (`.dev` and `.post` suffixes, epochs) are never candidates, and neither are yanked releases.

## Checksums

Every uploaded file carries a sha256 digest in the listing itself, so a [follower](checksums.md) can keep a checksum in lockstep with the version without downloading anything, selecting the file with `pattern`:

```toml
# clover: provider=pypi package=uv-build id=uv-build
uv-build-version = "0.11.16"

# clover: from=uv-build value=sha256 pattern=uv_build-<version>.tar.gz
uv-build-sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step, and a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
