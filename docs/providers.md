# Providers

A provider is the source of truth a directive resolves against. Set it with the `provider` key, or omit it and let Clover [infer one](auto.md) from the target line.

| Provider    | Tracks                                                       | Page                                 |
| ----------- | ------------------------------------------------------------ | ------------------------------------ |
| `auto`      | Inferred from the target line's contents                     | [Auto-detection](auto.md)            |
| `docker`    | Image tags and digests from a container registry             | [Docker](docker.md)                  |
| `gitea`     | Tags and releases of a Gitea/Forgejo repository (Codeberg)   | [Gitea](gitea.md)                    |
| `github`    | Releases, tags, and branch commits of a GitHub repository    | [GitHub](github.md)                  |
| `gitlab`    | Tags and releases of a GitLab project                        | [GitLab](gitlab.md)                  |
| `go`        | Go toolchain releases from the go.dev download index         | [Go](go.md)                          |
| `hashicorp` | Release versions of a HashiCorp tool (Terraform, Vault, ...) | [HashiCorp](hashicorp.md)            |
| `helm`      | Chart versions from a classic or OCI repository              | [Helm](helm.md)                      |
| `http`      | Versions extracted from an arbitrary HTTP endpoint           | [HTTP](http.md)                      |
| `manual`    | A value you maintain by hand, published for followers        | [Manual](manual.md)                  |
| `node`      | Node.js runtime versions from nodejs.org                     | [Node.js](node.md)                   |
| `npm`       | Package versions from the npm registry                       | [npm](npm.md)                        |
| `opentofu`  | Terraform provider versions from the OpenTofu registry       | [Terraform / OpenTofu](terraform.md) |
| `python`    | CPython interpreter releases from python.org                 | [Python](python.md)                  |
| `terraform` | Terraform provider versions from a provider registry         | [Terraform / OpenTofu](terraform.md) |
| `zig`       | Zig toolchain releases from the ziglang.org download index   | [Zig](zig.md)                        |

A directive with no `provider` and a `from` key is a **follower**, which reads a value resolved elsewhere instead of contacting an upstream. See [Following values](following.md).
