# clover

> Keep your versions evergreen.

clover keeps version references in a codebase synchronised with their upstream sources of truth. You annotate a line with a `clover:` comment describing where its version comes from, and clover resolves the latest matching version and rewrites the line in place - across Dockerfiles, YAML, HCL, shell, Markdown, or any other text format.

## Why

Version strings rot. A pinned image tag, a GitHub release, a checksum - each drifts out of date the moment upstream moves, and there's rarely a single place that lists them all. clover inverts the problem: the instruction to keep a line fresh lives in a comment *right next to that line*, so there's no central manifest to maintain and intent never gets separated from the code it governs.

## How it works

Place a `clover:` annotation in an ordinary comment next to the line you want kept up to date:

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

clover scans your files, finds every annotation, resolves each one against its provider, and updates the adjacent line - deterministically and atomically. Lines without a `clover:` comment are never touched.

The annotation is a set of `key=value` pairs. Common keys:

- `provider` - where the version comes from (`github`, `docker`, or `auto` to infer it from the line).
- `repository` / `image` - the upstream source to track.
- `constraint` - how far to allow updates (e.g. `minor`).

Sensible patterns are inferred from the existing content of the target line, so common cases need almost no configuration.

### Tracking floating refs

Some references are not versions at all: a Docker tag like `latest` or `nonroot`, or a GitHub Action pinned to a branch HEAD. These move in place - the tag or branch name stays, but the digest or commit it points at drifts. `track` keeps that secure pin fresh without selecting a new version: `track=*` infers the ref already on the line, or name it explicitly (`track=nonroot`, `track=main`).

```dockerfile
# clover: provider=docker track=*
FROM redis:latest@sha256:0000000000000000000000000000000000000000000000000000000000000000
```

```yaml
# clover: provider=github track=main verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

clover re-resolves the digest (Docker) or commit (GitHub) each run, leaving the `latest`/`main` text untouched. `track` replaces the selection stage, so it cannot be combined with selection keys (`constraint`, `include`/`exclude`, `behind`, `prerelease`, `allow-downgrade`); `cooldown` still applies, holding back a target that is too fresh, and `verify`/`verify-branch` still cross-check the resolved pin.

## Install

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

## Usage

```console
clover init        # create a starter .clover.yaml interactively
clover run         # resolve references and update them in place
clover run -n      # dry-run: resolve and preview, write nothing
clover lint        # check every directive resolves, offline, no writes
clover format      # canonicalise directive comments
```

`run` accepts paths to scan and flags such as `--tag` (process only matching directives), `--deep` (follow pagination for accuracy), and `--[no-]prerelease`. See `clover run --help` for the full set.

Some providers need authentication for higher rate limits or private sources:

```console
clover login       # authenticate with a provider via its device flow
```

## Documentation

- [docs/SPEC.md](docs/SPEC.md) - conceptual specification.
- [docs/DESIGN.md](docs/DESIGN.md) - design notes.
