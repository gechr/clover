# clover

> Keep your versions evergreen.

clover keeps version references in a codebase synchronised with their upstream sources of truth. You annotate a line with a `clover:` comment describing where its version comes from, and clover resolves the latest matching version and rewrites the line in place - across Dockerfiles, YAML, HCL, shell, Markdown, or any other text format.

## How it works

Place a `clover:` annotation in an ordinary comment next to the line you want kept up to date:

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

clover scans your files, finds every annotation, resolves each one against its provider, and updates the adjacent line - deterministically and atomically. Lines without a `clover:` comment are never touched.

## Installation

```sh
brew install gechr/tap/clover
```

```sh
go install github.com/gechr/clover@latest
```

## Quick start

```console
clover init        # create a starter .clover.yaml interactively
clover run         # resolve references and update them in place
clover run -n      # dry-run: resolve and preview, write nothing
clover lint        # check every directive resolves, offline, no writes
clover format      # canonicalise directive comments
```

## Documentation

- [Specification](SPEC.md) - the conceptual specification.
- [Design](DESIGN.md) - design notes.
