# Verification

When a line carries a secure pin (a Docker digest or a GitHub commit), Clover can deep-verify that the pin genuinely corresponds to the ref it claims, rather than trusting it blindly.

<!-- clover-lint-skip -->

```yaml
# clover: provider=github track=main verify=true verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

## Keys

| Key             | Description                                                                                                      |
| --------------- | ---------------------------------------------------------------------------------------------------------------- |
| `verify`        | Deep-verify this annotation's secure pin against upstream                                                        |
| `verify-branch` | The allowed source-branch glob (or `/regex/`) for the verification. Defaults to the repository's default branch. |

`verify-branch` is what lets Clover confirm that the commit a tag points at actually belongs to the branch you expect, which is useful when a tag is cut from a release branch rather than the default one.

Verification pairs naturally with [`track`](tracking.md). Tracking keeps the pin fresh, and `verify` checks whether each freshly resolved pin is reachable from an allowed branch. A verification failure blocks the update. The line keeps its current value, any markers that follow the blocked one hold too, and `clover run` exits non-zero. A tag whose commit shares no history with the repository at all (an impostor commit) fails the same way. If the tag legitimately lives on a release branch, widen the allowed set with `verify-branch`.
