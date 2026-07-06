# clover

Clover keeps version references synchronized with their upstream sources of truth. You annotate a line with a `clover:` comment describing where its version comes from, and Clover resolves the latest matching version and rewrites the line in place. It works across Dockerfiles, YAML, HCL, shell, Markdown, and any other text format.

## How it works

Place a `clover:` annotation in an ordinary comment next to the line you want kept up to date:

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

Clover scans your files, finds every annotation, resolves each one against its provider, and updates the adjacent line, deterministically and atomically. Lines without a `clover:` comment are never touched.

## Installation

### macOS / Linux

```bash
brew install gechr/tap/clover
```

### Windows

```bash
scoop bucket add gechr https://github.com/gechr/scoop-bucket
scoop install gechr/clover
```

### Go

```bash
go install github.com/gechr/clover@latest
```

## Quick start

```bash
# create a starter .clover.yaml interactively
clover init

# dry-run: resolve and preview, write nothing
clover run --dry-run

# resolve references and update them in place
clover run

# check every directive resolves, offline, no writes
clover lint

# canonicalize directive comments
clover format
```
