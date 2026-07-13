# Verification

When a line carries a secure pin (a Docker digest or a forge commit), Clover can verify that the pin genuinely corresponds to the ref it claims, rather than trusting it blindly. Commit pins use repository history; Docker digest pins can additionally require a Sigstore attestation from an expected identity.

## Impostor detection (the default)

Every commit pin resolved with a credential is checked against the repository's branches automatically. The commit a tag points at must be reachable from at least one branch. A tag pointing at a commit outside every branch's history is the signature of a maliciously published tag, and it blocks the update. This tier needs no configuration, and because a tag cut from any branch passes, ordinary release engineering such as release branches and backports never trips it. Anonymous lookups skip the tier rather than spend the small unauthenticated rate limit on it. Set `verify=false` on a marker to opt it out.

## Allowed branches (`verify`)

The strict tier confirms the commit belongs to a specific set of branches, not merely to the repository. Set `verify=true` to check the resolved commit against the repository's default branch.

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=main verify=true
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

### Keys

| Key               | Description                                                                                                      |
| ----------------- | ---------------------------------------------------------------------------------------------------------------- |
| `verify`          | Deep-verify this annotation's secure pin against upstream                                                        |
| `verify-branch`   | The allowed source-branch glob (or `/regex/`) for the verification. Defaults to the repository's default branch. |
| `verify-identity` | Signer certificate SAN glob or `/regex/` a digest pin's Sigstore attestation must match                          |
| `verify-issuer`   | OIDC issuer URL for `verify-identity`. Defaults to GitHub Actions.                                               |

`verify-branch` narrows the check to the branch you expect, which is useful when a tag is cut from a release branch rather than the default one. Setting it also enables the strict tier on its own, so `verify=true` is unnecessary alongside it.

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=release-1.x verify-branch=release-*
- uses: actions/checkout@0000000000000000000000000000000000000000 # release-1.x
```

Verification pairs naturally with [`track`](tracking.md). Tracking keeps the pin fresh, and `verify` checks whether each freshly resolved pin is reachable from an allowed branch.

## Attestation identity (`verify-identity`)

Docker digest pins can require a modern Sigstore bundle published through the registry's OCI referrers API. Set `verify-identity` to the expected signing certificate's subject alternative name, using the same whole-string glob or `/regex/` syntax as `verify-branch`:

```dockerfile
# clover: provider=docker repository=owner/app registry=ghcr.io verify-identity=https://github.com/owner/app/.github/workflows/*
FROM ghcr.io/owner/app:1.2.3@sha256:0000000000000000000000000000000000000000000000000000000000000000
```

The certificate issuer defaults to `https://token.actions.githubusercontent.com`. Set `verify-issuer` alongside `verify-identity` for another OIDC issuer. Clover accepts any cryptographically valid modern Sigstore bundle for the pinned digest that matches both values; SLSA provenance is accepted but not required. Legacy cosign `sha256-*.sig` signatures are not supported.

The check verifies the newly resolved digest before writing an update and also fails closed when no matching bundle exists. A provider that cannot fetch and verify attestations rejects the marker rather than silently skipping it. `verify=false` and `--no-verify` suppress this check along with the other verification tiers.

Publishers commonly attest a multi-architecture index digest, not each platform manifest. A marker combining `platform` with `verify-identity` therefore usually fails unless the publisher also attests the per-platform digest. Leave `platform` unset to verify the index digest.

## Failures block the update

A verification failure blocks the update. The line keeps its current value, any markers that follow the blocked one hold too, and `clover run` exits non-zero. If a tag legitimately lives on a release branch and the strict commit tier rejects it, widen the allowed set with `verify-branch`.
