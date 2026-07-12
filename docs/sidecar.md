# Sidecars

A `clover:` directive is a comment, so a file with no comment syntax has nowhere to host one. Strict JSON files like `package.json` and `tsconfig.json` are the usual case, and plain-text pins like pyenv's `.python-version` share the problem. A **sidecar** carries the directives out of band instead. It is a YAML file beside the target that names each line to track and how to track it.

```yaml
# tsconfig.json.clover.yaml
- provider: github
  repository: biomejs/biome
  tag-prefix: "@biomejs/biome@"
  constraint: minor
  jq: '.["$schema"]'
```

`clover run`, `clover lint`, and `clover format` discover sidecars automatically, and the target itself is rewritten in place, exactly as if the directive had been an inline comment. `run` never writes to the sidecar, only to the target.

## Naming

A sidecar is named `<target>.clover.yaml` and lives in the same directory as its target. `tsconfig.json` is tracked by `tsconfig.json.clover.yaml`, and `config/app.json` by `config/app.json.clover.yaml`. The target must exist. A sidecar whose target is missing is reported as an error rather than silently ignored.

A bare `.clover.yaml` (with nothing before the suffix) is Clover's [project configuration](configuration.md), never a sidecar.

## Entries

A sidecar is a YAML **list**, one item per directive. Each item uses the same vocabulary you would write inline, expressed as native YAML keys rather than a flat `key=value` string:

```yaml
# package.json.clover.yaml
- provider: docker
  repository: redis
  jq: '.["services"]["cache"]["image"]'
  find: redis:<version>
```

A key that may repeat inline ([`include`](filtering.md), [`exclude`](filtering.md)) is written as a sequence, and [`tags`](annotations.md#disabling-and-filtering) accepts a sequence too:

```yaml
- provider: github
  repository: cli/cli
  jq: .["version"]
  exclude:
    - "*-beta*"
    - "*-rc*"
  tags: [infra, cli]
```

Every key from [Annotations](annotations.md#keys) is valid in a sidecar, plus the `jq` locator below. The exceptions are the [`target` and `offset` anchors](annotations.md#anchoring-the-target-line), which are relative to a comment line and so have no meaning here.

## Locators

An inline directive governs the line below it (or the first match of its [`target` anchor](annotations.md#anchoring-the-target-line)). A sidecar entry has no such adjacency, so it must name its own target line. Every entry therefore carries a locator, either `jq`, `find`, or both:

- **`find`** is a glob (with `<version>` and other [placeholders](find-replace.md)) or a `/regex/`, matched against the file's lines. It works for **any** comment-less format and selects the one line whose content matches.
- **`jq`** is a [jq](https://jqlang.org) path expression (e.g. `.["$schema"]`, `.dependencies.react`) evaluated against the target as JSON. It is JSON-only and **recommended for JSON** because it is robust against the same version string appearing twice, against reformatting, and against a key moving. Clover resolves the path to a line without ever re-serializing the JSON, so the file's formatting and key order are preserved.

They **compose**. When both are present, `jq` selects the line and `find` refines the region within it, which is useful when a version is embedded in a longer string such as a `$schema` URL:

```yaml
# tsconfig.json.clover.yaml
- provider: github
  repository: biomejs/biome
  tag-prefix: "@biomejs/biome@"
  jq: '.["$schema"]'
  find: schemas/<version>/schema.json
```

Locators are deterministic and fail loud, matching the rest of Clover. A locator that resolves to zero lines, or to more than one, is an error rather than a guess.

## Conflicts with inline directives

A commentable file (a `.lock`, a Dockerfile) can carry both inline directives and a sidecar. When a sidecar entry resolves to a line that is already governed:

- A line under [`clover:ignore`](ignore.md) wins. The sidecar entry is skipped with a warning, since an external sidecar must not override a local opt-out.
- A line that already has an inline `clover:` directive is a conflict, since two directives governing one line is non-deterministic. This is a hard error at `lint` and a skip with a warning at `run`. The same rule covers two sidecar entries that resolve to the same line.

## Editor schema

Clover publishes a [JSON schema](https://raw.githubusercontent.com/gechr/clover/main/internal/sidecar/schema.json) for sidecars. Point your editor's YAML language server at it with a modeline at the top of the file to get completion and validation as you type:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/gechr/clover/main/internal/sidecar/schema.json
- provider: docker
  repository: redis
  jq: .["image"]
```

## Generating and formatting

A sidecar can be written by hand, or generated. [`clover annotate`](commands.md) scans a comment-less target (a strict-JSON file, or a pyenv `.python-version`) for trackable lines and generates a sidecar, previewing by default and writing with `--write`. [`clover format`](commands.md) canonicalizes an existing sidecar, sorting each entry's keys into their canonical order while preserving your comments.
