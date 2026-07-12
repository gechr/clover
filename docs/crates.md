# crates.io

The crates provider tracks the versions of a Rust crate published on [`crates.io`](https://crates.io), reading the registry API (`crates.io/api/v1/crates/<crate>/versions`) that lists a crate's whole version history. It resolves crate versions pinned outside cargo's reach, such as a `cargo install` in a CI workflow or a tool version in a script.

```yaml
steps:
  # clover: provider=crates package=cargo-audit
  - run: cargo install cargo-audit --locked --version 0.22.2
```

Clover deliberately does not manage `Cargo.toml` dependency tables. Those belong to cargo itself, which resolves compatible versions through `Cargo.lock` (`cargo update`) and bumps the requirements across majors (`cargo update --breaking`, `cargo upgrade`).

## Keys

| Key                            | Description                                                               |
| ------------------------------ | ------------------------------------------------------------------------- |
| `provider`                     | `crates`                                                                  |
| `package`                      | The crate name, as published on crates.io. **Required.**                  |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range) |
| [`include`](filtering.md)      | Keep only matching versions                                               |
| [`exclude`](filtering.md)      | Drop matching versions                                                    |
| [`cooldown`](cooldown.md)      | Require a minimum age before a version is eligible                        |

The registry API is public, so the crates provider needs no authentication. It is selected explicitly with `provider=crates package=<name>`, and the name is looked up exactly as published - crates.io does not fold case or separators.

Every version carries its publish time, so [`cooldown`](cooldown.md) works: a version is held back until it has aged past the window. The whole version history arrives in one response, so Clover always sees every release and `--deep` has nothing extra to fetch.

Cargo enforces semver on publish, so prereleases arrive in canonical form (`4.0.0-rc.3`) and are treated as [prereleases](prereleases.md), excluded by default and selected only when prereleases are allowed. Yanked versions are never candidates.

## Checksums

Every version carries the sha256 checksum of its `.crate` file in the listing itself, so a [follower](checksums.md) can keep a checksum in lockstep with the version without downloading anything, selecting the file with `pattern`:

```toml
# clover: provider=crates package=cargo-audit id=cargo-audit
audit-version = "0.22.2"

# clover: from=cargo-audit value=sha256 pattern=cargo-audit-<version>.crate
audit-sha256 = "0000000000000000000000000000000000000000000000000000000000000000"
```

The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step, and a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
