# Verification

When a line carries a secure pin (a Docker digest or a GitHub commit), Clover verifies that the pin genuinely corresponds to the ref it claims, rather than trusting it blindly. Verification runs at two tiers.

## Impostor detection (the default)

Every GitHub commit pin resolved with a credential is checked against the repository's branches automatically. The commit a tag points at must be reachable from at least one branch. A tag pointing at a commit outside every branch's history is the signature of a maliciously published tag, and it blocks the update. This tier needs no configuration, and because a tag cut from any branch passes, ordinary release engineering such as release branches and backports never trips it. Anonymous lookups skip the tier rather than spend the small unauthenticated rate limit on it. Set `verify=false` on a marker to opt it out.

## Allowed branches (`verify`)

The strict tier confirms the commit belongs to a specific set of branches, not merely to the repository.

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=main verify=true verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

### Keys

| Key             | Description                                                                                                      |
| --------------- | ---------------------------------------------------------------------------------------------------------------- |
| `verify`        | Deep-verify this annotation's secure pin against upstream                                                        |
| `verify-branch` | The allowed source-branch glob (or `/regex/`) for the verification. Defaults to the repository's default branch. |

`verify-branch` is what lets Clover confirm that the commit a tag points at actually belongs to the branch you expect, which is useful when a tag is cut from a release branch rather than the default one.

Verification pairs naturally with [`track`](tracking.md). Tracking keeps the pin fresh, and `verify` checks whether each freshly resolved pin is reachable from an allowed branch.

## Failures block the update

A verification failure at either tier blocks the update. The line keeps its current value, any markers that follow the blocked one hold too, and `clover run` exits non-zero. If a tag legitimately lives on a release branch and the strict tier rejects it, widen the allowed set with `verify-branch`.
