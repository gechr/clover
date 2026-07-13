<h1 align="center">🍀 Clover</h1>

Clover keeps version references in a codebase synchronized with their upstream sources of truth. Annotate a line with a `clover:` comment describing where its version comes from, and Clover resolves the latest matching version and rewrites the line in place. It works across Dockerfiles, YAML, HCL, shell, Markdown, and any other text format.

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
clover init        # create a starter .clover.yaml interactively
clover run         # resolve references and update them in place
clover run -n      # dry-run: resolve and preview, write nothing
clover lint        # check every directive resolves, offline, no writes
clover format      # canonicalize directive comments
clover annotate    # preview @clover directives for recognized lines
clover annotate -w # write those annotations
```

By default, lines without a `clover:` comment are never touched.

## Providers

Clover resolves versions from a range of upstream sources:

- **`crates`** - Rust crate versions from [crates.io](https://crates.io/)
- **`docker`** - image tags and digests from any OCI registry
- **`gitea`** - Gitea and Forgejo forges, defaulting to [Codeberg](https://codeberg.org/)
- **`github`** - releases and tags, with checksum and commit-SHA pinning
- **`gitlab`** - project tags and releases
- **`go`** - Go toolchain versions from [go.dev](https://go.dev/)
- **`hashicorp`** - Terraform, Vault, Consul, Nomad, and other HashiCorp tools from [releases.hashicorp.com](https://releases.hashicorp.com/)
- **`helm`** - chart versions from HTTP or OCI repositories
- **`http`** - any endpoint, read with [`jq`](https://jqlang.org/) or a regular expression
- **`manual`** - a hand-maintained value for other directives to follow
- **`node`** - Node.js runtime versions from [nodejs.org](https://nodejs.org/)
- **`npm`** - package versions from [registry.npmjs.org](https://registry.npmjs.org/)
- **`pypi`** - Python package versions from [pypi.org](https://pypi.org/)
- **`python`** - Python runtime versions from [python.org](https://www.python.org/)
- **`rust`** - Rust toolchain versions, stable or beta channel
- **`swift`** - Swift toolchain versions from [swift.org](https://www.swift.org/)
- **`terraform`** / **`opentofu`** - provider plugins from a registry
- **`zig`** - Zig toolchain versions from [ziglang.org](https://ziglang.org/)

A bare [`# @clover`](https://gechr.github.io/clover/auto.html#the-clover-shorthand) comment (shorthand for `provider=auto`) infers the right provider from the line itself, so common cases such as GitHub Actions, `FROM` images, `go.mod`, and Terraform blocks need no provider at all.

## Documentation

Full documentation is available at [gechr.github.io/clover](https://gechr.github.io/clover/).
