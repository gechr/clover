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

### Formats without comments

A directive is a comment, so a format that has no comment syntax can't host one. Strict JSON is the usual culprit: `package.json`, `tsconfig.json`, and the like have nowhere to put a `clover:` line. If the tool also accepts JSONC (JSON with comments), rename the file to `.jsonc` and add a `//` directive.

[Biome](https://biomejs.dev), for example, reads `biome.jsonc` natively, so its `$schema` URL can track Biome's releases:

```jsonc
// clover: provider=github repository=biomejs/biome tag-prefix=@biomejs/biome@ constraint=minor
"$schema": "https://biomejs.dev/schemas/2.4.14/schema.json"
```

No [`find`](find-replace.md) is needed - the version is the only version-shaped token on the line, so Clover locates it automatically. The [`tag-prefix`](filtering.md#tag-prefix) scopes selection to Biome's monorepo-scoped `@biomejs/biome@` tags.

## Keys

Every annotation is a flat list of space-separated `key=value` pairs. The available keys fall into a few groups:

| Group         | Keys                                                                                                                                                                                                                      |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source        | [`provider`](providers.md), `repository`, `registry`, `source`, [`platform`](docker.md)                                                                                                                                   |
| Selection     | [`constraint`](constraints.md), [`include`/`exclude`](filtering.md), [`tag-prefix`](filtering.md), [`asset`](github.md), [`behind`](filtering.md), [`prerelease`](prereleases.md), [`cooldown`](cooldown.md), `downgrade` |
| Floating refs | [`track`](tracking.md), [`verify`, `verify-branch`](verification.md)                                                                                                                                                      |
| Links         | [`id`, `from`, `select`, `value`](following.md)                                                                                                                                                                           |
| Side values   | [`value`, `sha256-source`, `sha256-url`, `pattern`](checksums.md)                                                                                                                                                         |
| Matching      | [`find`, `replace`](find-replace.md), `skip`, `tags`                                                                                                                                                                      |

You rarely need most of them. Clover infers sensible patterns from the existing content of the target line, so a `provider` and a `repository` are usually enough.

## Matching the value

By default Clover finds the version on the target line by inspecting its existing content and preserves that line's style - a leading `v`, the number of components, and recognized suffixes all stay as written. When the target is unusual enough that automatic matching can't pin the right region, or you want to rewrite more than the bare version, set [`find` and `replace`](find-replace.md) to take explicit control.

## Disabling and filtering

- `skip` - disable an annotation without deleting it.
- `tags` - attach comma-separated labels so `clover run --tag <tag>` can process a subset.

```dockerfile
# clover: provider=docker repository=redis constraint=minor tags=infra
FROM redis:7.2.0
```
