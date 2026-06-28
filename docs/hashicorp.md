# HashiCorp

The HashiCorp provider tracks the release versions of a HashiCorp tool from HashiCorp's public releases service (`releases.hashicorp.com`), the source of truth for Terraform, Vault, Consul, Nomad, Packer, Boundary, Sentinel, and the rest.

```yaml
# clover: provider=hashicorp product=terraform constraint=minor
terraform_version: 1.9.8
```

## Keys

| Key                                   | Description                                                                                                 |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `provider`                            | `hashicorp`                                                                                                 |
| `product`                             | The product slug to track (e.g. `terraform`, `vault`, `consul`, `nomad`, `packer`, `boundary`, `sentinel`). |
| `enterprise`                          | Track enterprise-licensed releases, rendering the bare semver. Defaults to `false`.                         |
| `build`                               | Track a specific enterprise build flavor by its build-metadata suffix, rendering the full version.          |
| [`constraint`](constraints.md)        | How far the version may move (`major`/`minor`/`patch`, or a semver range).                                  |
| [`include` / `exclude`](filtering.md) | Filter the candidate versions.                                                                              |
| [`prerelease`](prereleases.md)        | Allow or exclude prerelease versions (alphas, betas, and release candidates).                               |
| [`cooldown`](cooldown.md)             | Require a minimum age before a version is eligible.                                                         |

The releases service is public, so the HashiCorp provider needs no authentication. It is selected explicitly with `provider=hashicorp` - a bare version line carries no signal to [infer](auto.md) it from.

## Editions

By default Clover tracks a product's open-source releases. HashiCorp also publishes enterprise builds, whose versions carry a `+` build-metadata suffix (`+ent`, and combinations like `+ent.hsm`, `+ent.fips1403`, or `+ent.musl`). Two keys reach them:

- `enterprise=true` tracks the enterprise releases but renders the **bare** semver, collapsing every flavor of a release to one version. Use it when your line holds just the number:

  ```yaml
  # clover: provider=hashicorp product=vault enterprise=true constraint=minor
  vault_version: 1.21.8
  ```

- `build=<suffix>` tracks one exact flavor and renders the **full** version, suffix included. The artifact filename embeds this suffix, so this is the form to track when your line pins a real download:

  ```yaml
  # clover: provider=hashicorp product=vault build=ent.hsm.fips1403 constraint=minor
  vault_version: 1.21.8+ent.hsm.fips1403
  ```

The suffix is whatever HashiCorp publishes for that product - for example Consul's FIPS builds are tagged `ent.fips1402` while Vault's are `ent.fips1403`, and Nomad ships an `ent.musl` flavor. A suffix that matches no release simply yields no candidates.

## Checksums

HashiCorp publishes a `SHA256SUMS` file for every release at a predictable URL, so a [follower](checksums.md) can keep a checksum in lockstep with the version by templating [`sha256-url`](checksums.md#sourcing-a-sha256) with `<version>` and selecting the artifact with `pattern`:

```yaml
# clover: provider=hashicorp product=terraform id=tf constraint=minor
terraform_version: 1.9.8

# clover: from=tf value=sha256 sha256-url=https://releases.hashicorp.com/terraform/<version>/terraform_<version>_SHA256SUMS pattern=terraform_<version>_linux_amd64.zip
terraform_sha256: 0000000000000000000000000000000000000000000000000000000000000000
```

The same follower works for a `build=` flavor: `<version>` resolves to the full value, so the `+ent.hsm.fips1403` suffix is substituted into the `SHA256SUMS` URL and matched by the `<version>` glob token in `pattern`. The checksum is refreshed only when the version it follows actually changes, so the two never drift out of step - a digest that was once pinned never moves on its own. Pass `--force` (or set `run.force`) to deliberately re-pin it when an unchanged version's artifact was legitimately re-published.
