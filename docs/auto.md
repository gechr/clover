# Auto-detection

When you omit `provider` (or set `provider=auto`), Clover infers the provider and its parameters from the content of the target line. Common cases need almost no annotation.

<!-- clover-lint-skip -->

```dockerfile
# clover: constraint=minor
FROM redis:7.2.0
```

Here Clover recognizes a Docker image reference on the line and resolves it with the [Docker](docker.md) provider, inferring `repository=redis`. A line that names a GitHub repository resolves with the [GitHub](github.md) provider instead.

## Recognized shapes

Auto-detection recognizes:

- A GitHub Actions `uses:` reference in YAML, pinned to a commit SHA or to a tag, resolved by the [GitHub](github.md) provider with the inferred `repository`.
- A `FROM` instruction in a Dockerfile or Containerfile, tag-only or digest-pinned, resolved by the [Docker](docker.md) provider with the inferred `registry` and `repository`.
- An `image:` mapping in YAML, tag-only or digest-pinned, resolved the same way.
- A workflow container job's `uses: docker://` reference, tag-only or digest-pinned, resolved the same way.
- A digest-pinned image whose tag is a floating name (`nonroot`, `latest`), on any of the lines above, resolved by the [Docker](docker.md) provider with the inferred [`track`](tracking.md), so the digest stays fresh while the tag text stays put.
- A GitLab CI/CD component include (`component: gitlab.com/group/project/name@1.0.0`), resolved by the [GitLab](gitlab.md) provider with the inferred `repository`, and the inferred `host` when the component lives on a self-managed instance.
- A tool version in a [mise](https://mise.jdx.dev) configuration file (`.mise.toml` or `mise.toml`). A HashiCorp product like `terraform = "1.9.8"` resolves with the [HashiCorp](hashicorp.md) provider and its inferred `product`, `node` resolves with the [Node.js](node.md) provider, and a `github:` or `ubi:` backend key (or one of the several hundred registry tools released on GitHub, like `tofu`, `go`, or `ripgrep`) resolves with the [GitHub](github.md) provider and its inferred `repository`. A tool released on Codeberg, like `zig`, resolves with the [Gitea](gitea.md) provider. The tool-to-repository map is generated from the [mise registry](https://mise.jdx.dev/registry.html).
- The `go` directive in a `go.mod` file, resolved by the [GitHub](github.md) provider against `golang/go` with `tag-prefix=go`, since Go releases are tagged `goX.Y.Z`.
- A `required_version` constraint in a Terraform file, resolved by the [HashiCorp](hashicorp.md) provider with `product=terraform`. The version inside the constraint is bumped in place, so `"~> 1.11.0"` keeps its operator and precision. In a `.tofu` file the same constraint tracks [OpenTofu's releases](https://github.com/opentofu/opentofu) with the [GitHub](github.md) provider instead. An OpenTofu configuration written in `.tf` files should set `provider` explicitly, since the line itself cannot say which toolchain it pins.
- A `version` constraint inside a `required_providers` entry, resolved by the [Terraform](terraform.md) provider with the `source` read from the entry's own block - the one inference that looks beyond the target line, parsing the file as HCL to find the sibling `source` attribute. A `version` outside `required_providers` (a module block's, say) infers nothing. In a `.tofu` file the entry resolves with `provider=opentofu` against the OpenTofu registry; an OpenTofu repository written in `.tf` files sets `provider=opentofu` explicitly.

## When to be explicit

Auto-detection covers the obvious cases. Set `provider` explicitly when:

- the line is ambiguous, or the value you want to track is not the most obvious token on it,
- you are tracking something that is not literally written on the target line,
- you want the annotation to document intent regardless of the line's contents.

Inference only fills in what you leave out, and any key you set yourself always wins.

## Generating annotations

To add `provider=auto` directives across an existing codebase rather than write them by hand, run [`clover annotate`](commands.md#annotate). It scans for the same lines auto-detection recognizes and inserts a directive above each, so onboarding a repository is a single command.
