# Annotations

An annotation is a `clover:` comment that tells Clover how to keep the next line up to date. You write it in the host file's ordinary comment syntax, and Clover reads it as a set of `key=value` pairs.

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

The comment governs the **target line** - by default the line immediately below it. Lines without a `clover:` comment are never touched.

## Comment syntax

Clover treats files as plain text and recognizes the comment style of whatever format it finds the annotation in - `#` for Dockerfiles, YAML, and shell, `//` for HCL and Go, `<!-- -->` for Markdown and HTML, and so on. The directive is everything after the `clover:` keyword.

```yaml
# clover: provider=docker repository=redis constraint=minor
image: redis:7.2.0
```

```hcl
// clover: provider=github repository=hashicorp/terraform constraint=minor
required_version = "1.7.0"
```

## Keys

Every annotation is a flat list of space-separated `key=value` pairs. The available keys fall into a few groups:

| Group         | Keys                                                                                                                                                                        |
| ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source        | [`provider`](providers.md), `repository`, `registry`                                                                                                                        |
| Selection     | [`constraint`](constraints.md), [`include`/`exclude`](filtering.md), [`behind`](filtering.md), [`prerelease`](prereleases.md), [`cooldown`](cooldown.md), `downgrade` |
| Floating refs | [`track`](tracking.md), [`verify`, `verify-branch`](verification.md)                                                                                                        |
| Links         | [`id`, `from`, `select`, `value`](following.md)                                                                                                                             |
| Side values   | [`value`, `sha256-source`, `sha256-url`, `pattern`](checksums.md)                                                                                                           |
| Matching      | `find`, `replace`, `skip`, `tags`                                                                                                                                           |

You rarely need most of them. Clover infers sensible patterns from the existing content of the target line, so a `provider` and a `repository` are usually enough.

## Matching the value

By default Clover finds the version on the target line by inspecting its existing content and preserves that line's style - a leading `v`, the number of components, and recognized suffixes all stay as written. When the target needs an explicit pattern, set `find` (a glob with `<placeholders>`, or a `/regex/`) and `replace` (the template that renders the new line).

## Disabling and filtering

- `skip` - disable an annotation without deleting it.
- `tags` - attach comma-separated labels so `clover run --tag <tag>` can process a subset.

```dockerfile
# clover: provider=docker repository=redis constraint=minor tags=infra
FROM redis:7.2.0
```
