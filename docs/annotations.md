# Annotations

An annotation is a `clover:` comment that tells Clover how to keep the next line up to date. You write it in the host file's ordinary comment syntax, and Clover reads it as a set of `key=value` pairs.

```dockerfile
# clover: provider=github repository=redis/redis constraint=minor
FROM redis:7.2.0
```

The comment governs the **target line**, which is by default the line immediately below it, or the first matching line below it when a [`target` anchor](#anchoring-the-target-line) is set. Lines without a `clover:` comment are never touched.

## Comment syntax

Clover treats files as plain text and recognizes the comment style of whatever format it finds the annotation in: `#` for Dockerfiles, YAML, and shell, `//` for HCL and Go, `<!-- -->` for Markdown and HTML, and so on. The directive is everything after the `clover:` keyword.

```yaml
# clover: provider=docker repository=redis constraint=minor
image: redis:7.2.0
```

```hcl
// clover: provider=github repository=hashicorp/terraform constraint=minor
required_version = "1.7.0"
```

The most common annotation by far is `provider=auto`, which asks Clover to infer everything from the target line. It has a dedicated shorthand, a bare [`@clover`](auto.md#the-clover-shorthand) comment, described with the rest of [auto-detection](auto.md).

A keyword whose colon is missing or detached ahead of pair-shaped text (`# clover foo=bar`, `# @clover : constraint=minor`) is reported as a malformed directive rather than silently ignored, while a comment that merely leads with the word stays inert.

### Formats without comments

A directive is a comment, so a format that has no comment syntax can't host one. Strict JSON is the usual culprit, and plain-text pins like pyenv's `.python-version` share the problem. `package.json`, `tsconfig.json`, and the like have nowhere to put a `clover:` line, so Clover reads their directives from a [**sidecar**](sidecar.md), a `<target>.clover.yaml` file beside the target that carries the same directives as native YAML keys.

[Biome](https://biomejs.dev), for example, ships a `biome.json` whose `$schema` URL can track Biome's releases. A `biome.json.clover.yaml` sidecar locates the line with [`jq`](sidecar.md#locators) and tracks it:

```yaml
# biome.json.clover.yaml
- provider: github
  repository: biomejs/biome
  tag-prefix: "@biomejs/biome@"
  constraint: minor
  jq: '.["$schema"]'
```

The [`jq`](sidecar.md#locators) locator names the line by JSON path, and since the version is the only version-shaped token on it, no [`find`](find-replace.md) is needed. The [`tag-prefix`](filtering.md#tag-prefix) scopes selection to Biome's monorepo `@biomejs/biome@` tags. See [Sidecars](sidecar.md) for the naming rule, both locators, and the editor schema.

If the tool also accepts JSONC (JSON with comments), an inline `//` directive in a `.jsonc` file is an alternative, but the sidecar leaves the JSON untouched and works even where renaming to `.jsonc` is not an option.

## Keys

Every annotation is a flat list of space-separated `key=value` pairs. The available keys fall into a few groups:

| Group         | Keys                                                                                                                                                                                                                      |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Source        | [`provider`](providers.md), `repository`, `registry`, `source`, [`platform`](docker.md)                                                                                                                                   |
| Selection     | [`constraint`](constraints.md), [`include`/`exclude`](filtering.md), [`tag-prefix`](filtering.md), [`asset`](github.md), [`behind`](filtering.md), [`prerelease`](prereleases.md), [`cooldown`](cooldown.md), `downgrade` |
| Verification  | [`verify`, `verify-branch`, `verify-identity`, `verify-issuer`](verification.md)                                                                                                                                          |
| Links         | [`id`, `from`, `select`, `value`](following.md)                                                                                                                                                                           |
| Side values   | [`value`, `sha256-source`, `sha256-url`, `pattern`](checksums.md)                                                                                                                                                         |
| Matching      | [`target`, `offset`](#anchoring-the-target-line), [`find`, `replace`](find-replace.md), `disabled`, `tags`                                                                                                                |

You rarely need most of them. Clover infers sensible patterns from the existing content of the target line, so a `provider` and a `repository` are usually enough.

## Anchoring the target line

When the comment can't sit directly above the value (a generated block, a license header that must stay first, a value buried a few lines into a mapping), two anchor keys move the target line:

- **`target`** is a glob or `/regex/`, and the comment governs the **first matching line below it**. Prefer this form, since the anchor follows the content and lines added or removed between the comment and the value never silently retarget the directive.
- **`offset`** is a fixed number of lines below the comment. The default is `1`, the line immediately below.

```yaml
# clover: provider=docker repository=redis constraint=minor target=image:*
metadata:
  name: redis
image: redis:7.2.0
```

They compose. `offset` sets where the `target` search **starts**, so `offset=3 target=image:*` skips any `image:` in the first two lines below the comment and anchors to the first match from the third. A `target` that matches no line below its comment, or an `offset` that is not a positive integer, is a lint error, and `clover run` reports it without touching the file.

The anchors pick the **line**, while [`find` and `replace`](find-replace.md) still control the region *within* that line, and everything composes. In a sidecar entry `target` and `offset` are invalid, since a sidecar names its line with its own [locators](sidecar.md#locators).

## Matching the value

By default Clover finds the version on the target line by inspecting its existing content and preserves that line's style. A leading `v`, the number of components, and recognized suffixes all stay as written. When the target is unusual enough that automatic matching can't pin the right region, or you want to rewrite more than the bare version, set [`find` and `replace`](find-replace.md) to take explicit control.

## Disabling and filtering

- `disabled` disables an annotation without deleting it.
- `tags` attaches comma-separated labels so `clover run --tag <tag>` can process a subset.

```dockerfile
# clover: provider=docker repository=redis constraint=minor tags=infra
FROM redis:7.2.0
```
