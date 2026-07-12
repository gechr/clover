# Terraform / OpenTofu

The Terraform provider tracks the versions of a Terraform provider (a plugin such as `hashicorp/aws`) from a registry implementing the [provider registry protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). It is registered twice: `provider=terraform` defaults to [`registry.terraform.io`](https://registry.terraform.io) and `provider=opentofu` defaults to [`registry.opentofu.org`](https://registry.opentofu.org), so an annotation names the ecosystem it belongs to. Both faces are one implementation, and `host` points either at a private registry.

```hcl
required_providers {
  aws = {
    source = "hashicorp/aws"
    # clover: provider=terraform source=hashicorp/aws constraint=minor
    version = "~> 6.39"
  }
}
```

The version inside the constraint string is the only version-shaped token on the line, so Clover bumps it in place and the operator and precision around it survive. [Auto-detection](auto.md) recognizes a `required_providers` version line on its own, reading the `source` from the enclosing entry, so a bare [`@clover`](auto.md#the-clover-shorthand) (the form `clover annotate` writes) is usually enough. It resolves against the Terraform registry; an OpenTofu repository sets `provider=opentofu` explicitly.

## Keys

| Key                            | Description                                                                         |
| ------------------------------ | ----------------------------------------------------------------------------------- |
| `provider`                     | `terraform` or `opentofu`                                                           |
| `source`                       | The provider's source address as `namespace/name`, e.g. `hashicorp/aws`             |
| `host`                         | The registry host, defaulting to `registry.terraform.io` or `registry.opentofu.org` |
| [`constraint`](constraints.md) | How far the version may move (`major`/`minor`/`patch`, or a semver range)           |
| [`include`](filtering.md)      | Keep only matching versions                                                         |
| [`exclude`](filtering.md)      | Drop matching versions                                                              |
| [`prerelease`](prereleases.md) | Allow or exclude prerelease versions (alphas, betas, and release candidates)        |

The registries are public, so the provider needs no authentication. The versions endpoint returns the whole version history in one response and carries no publication dates. Because [`cooldown`](cooldown.md) needs a date to measure age, a marker that sets one is skipped with a warning rather than updated past a gate Clover cannot check. To gate on age, track the provider's GitHub repository instead, where releases carry timestamps.

## Private registries

Any registry implementing the protocol works. Clover reads the host's `/.well-known/terraform.json` service discovery document to locate the `providers.v1` endpoint, exactly as `terraform init` does, so a registry that mounts the API elsewhere still resolves:

```hcl
# clover: provider=terraform source=acme/internal host=registry.example.com
version = "~> 2.4"
```
