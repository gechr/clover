<h1 align="center">🍀 Clover</h1>

Clover keeps version references in a codebase synchronized with their upstream sources of truth. Annotate a line with a `clover:` comment describing where its version comes from, and Clover resolves the latest matching version and rewrites the line in place - across Dockerfiles, YAML, HCL, shell, Markdown, or any other text format.

## Installation

### macOS / Linux

```shell
brew install gechr/tap/clover
```

### Windows

```shell
scoop bucket add gechr https://github.com/gechr/scoop-bucket
scoop install gechr/clover
```

### Go

```shell
go install github.com/gechr/clover@latest
```

## Quick Start

Place a `clover:` annotation in a comment next to the line you want kept up to date:

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

Then resolve and update every annotation in place:

```console
clover run         # resolve references and update them in place
clover run -n      # dry-run: resolve and preview, write nothing
clover lint        # check every directive resolves, offline, no writes
clover format      # canonicalize directive comments
```

Lines without a `clover:` comment are never touched.

## Documentation

Full documentation is available at [gechr.github.io/clover](https://gechr.github.io/clover/).
