# Find & replace

`find` and `replace` give you explicit control over **where** on the target line Clover rewrites and **what** it writes there. They override the automatic, shape-based matching that Clover normally applies.

For an ordinary version bump you won't usually need them: Clover already locates the version on the target line, preserves its style - a leading `v`, the number of components, recognized suffixes - and rewrites it in place. Reach for `find`/`replace` when the line is unusual enough that automatic matching can't pin the right region, or when you want to rewrite more than the bare version.

## `find` - locating the region

`find` is a pattern that matches the part of the line to rewrite. It comes in two dialects.

### Glob with placeholders

Any value that is not wrapped in `/` is a glob. Literal text matches itself, `*` matches any run, `?` matches any character, and a `<placeholder>` captures a token of a known shape:

| Token                         | Matches                              | Renders                           |
| ----------------------------- | ------------------------------------ | --------------------------------- |
| `<version>`                   | a full version, optional `v`, suffix | the new version, styled to match  |
| `<major.minor.patch>`         | `1.2.3`                              | the new `major.minor.patch`       |
| `<major.minor>`               | `1.2`                                | the new `major.minor`             |
| `<major>`                     | a single number                      | the new major                     |
| `<minor>`                     | a single number                      | the new minor                     |
| `<patch>`                     | a single number                      | the new patch                     |
| `<commit>`                    | a 40-character SHA                   | the new commit SHA                |
| `<sha256>`                    | a 64-character digest                | the new `sha256` digest           |
| `<hex>`                       | any run of hex digits                | match-only - preserved as found   |

`<hex>` is match-only: it lets you span a digest or build hash you don't want to touch, and Clover writes back whatever it captured.

```dockerfile
# clover: provider=github repository=acme/toolkit constraint=minor find=toolkit-<version>-linux
FROM toolkit-1.2.3-linux AS build
```

Only the version moves; the surrounding `toolkit-`/`-linux` context is preserved.

Result:

```dockerfile
FROM toolkit-1.5.0-linux AS build
```

`<version>` keeps the style of the text it replaced, so a shortened, `v`-prefixed reference stays that way:

```yaml
# clover: provider=docker repository=redis constraint=minor find=image:<version>
image:v1.2
```

The `v` prefix and the two-component precision are retained.

Result:

```yaml
image:v1.5
```

A match-only `<hex>` lets the version move while a build hash beside it is left intact:

```yaml
# clover: provider=github repository=acme/app constraint=minor find=<major.minor.patch>-<hex>.tgz
asset: 1.2.3-deadbeef.tgz
```

Result:

```yaml
asset: 1.5.0-deadbeef.tgz
```

### Regular expressions

A value wrapped in `/` is an unanchored RE2 regular expression. There are no placeholders - capture group 1 is the value Clover anchors selection on, and the whole match is the region it rewrites. Use this when the line's structure is easier to express as a regex than a glob.

```text
# clover: provider=github repository=acme/app constraint=minor find=/v(\d+\.\d+\.\d+)/
tag = v1.2.3
```

Result:

```text
tag = v1.5.0
```

## `replace` - rendering the result

`replace` is optional, and it changes how the matched region is rewritten.

**Without `replace`**, Clover substitutes each captured value in place and leaves everything else - literal text, separators, match-only captures - exactly as it found it. This is the behavior in every example above.

**With `replace`**, the entire matched region is re-rendered from the template. The template is literal text with `<token>` placeholders; each token expands to its resolved value, or - for a token that only appeared in `find` - to the text that was captured there. This lets you reorder, reformat, or rewrite several values at once.

A `replace` requires a `find` (there is nothing to match against otherwise), and a template that references a token Clover can't resolve for the candidate - say `<sha256>` when the source has no digest - is a hard error rather than a silent no-op.

For example, one source version can be rendered as both a short `major.minor` series and the full version, with literal text the original line doesn't contain:

```text
# clover: provider=github repository=acme/toolkit constraint=minor find=<version> replace="<major.minor> (<version>)"
release = 1.2.3
```

Result:

```text
release = 1.5 (1.5.0)
```

Because a template echoes tokens it didn't resolve, a match-only `<hex>` captured by `find` is carried across into the reformatted line:

```text
# clover: provider=github repository=acme/toolkit constraint=minor find=<major.minor.patch>+build.<hex> replace="v<major.minor.patch> (<hex>)"
ver = 1.2.3+build.deadbeef
```

Result:

```text
ver = v1.5.0 (deadbeef)
```

> **Secure pins don't need this.** You don't need `find`/`replace` to pin a GitHub Action (`uses: …@<sha> # vX`) or a Docker digest (`FROM …@sha256:…`). Clover recognizes those shapes and keeps the pin and its version comment in step on its own - see [pinning an action to a commit](github.md#pinning-an-action-to-a-commit).

## Notes

- `find`/`replace` can't be combined with [`track`](tracking.md) - tracking owns its own floating-ref locator.
- A glob has no path separators, so `*` spans every character including `/`.
- If `find` doesn't match the target line, the update will fail rather than trying to guess.
