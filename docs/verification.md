# Verification

When a line carries a secure pin - a Docker digest or a GitHub commit - Clover can deep-verify that the pin genuinely corresponds to the ref it claims, rather than trusting it blindly.

```yaml
# clover: provider=github track=main verify=true verify-branch=main
- uses: actions/checkout@0000000000000000000000000000000000000000 # main
```

## Keys

| Key             | Description                                                                                                      |
| --------------- | ---------------------------------------------------------------------------------------------------------------- |
| `verify`        | Deep-verify this annotation's secure pin against upstream                                                        |
| `verify-branch` | The allowed source-branch glob (or `/regex/`) for the verification. Defaults to the repository's default branch. |

`verify-branch` is what lets Clover confirm that the commit a tag points at actually belongs to the branch you expect - useful when a tag is cut from a release branch rather than the default one.

Verification pairs naturally with [`track`](tracking.md): tracking keeps the pin fresh, and `verify` reports whether each freshly-resolved pin is reachable from an allowed branch. A verification mismatch is reported as an error in the output, but it does not by itself block an otherwise resolved `clover run`.
