# Configuration

Most of Clover's behavior lives in the annotations themselves, so configuration is deliberately small. Clover reads from two layers that share the same keys:

- A **user** config under your XDG config directory at `clover/config.yaml` - typically `~/.config/clover/config.yaml`, or `$XDG_CONFIG_HOME/clover/config.yaml` when that variable is set. Use it for personal defaults that should apply everywhere.
- A **project** `.clover.yaml` at a repository's root, for settings that travel with the project.

The project config overlays the user one field by field: a key set in the project file wins, and anything it leaves unset falls back to the user value. Both files are optional, and both validate against the same schema.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/gechr/clover/main/internal/config/schema.json

required-version: ">=0.1.0"

paths:
  exclude:
    - vendor/**
    - "**/testdata/**"

global:
  output: wide # shared default for run and lint

run:
  verify: true # verify secure pins by default
  output: github # overrides global.output for `clover run`

fmt:
  prune: true

annotate:
  write: true # apply proposed annotations by default
```

## Options

Settings are grouped by the command they configure, with a `global` block for cross-command defaults. See [`.clover.reference.yaml`](../.clover.reference.yaml) for every key with its default.

| Key                | Description                                                                                                                                                                              |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `required-version` | A version constraint the running `clover` binary must satisfy (e.g. `">=0.1.0"`, `"~>0.1"`). Clover refuses to run if its own version falls outside the range.                           |
| `paths.exclude`    | [Doublestar](https://github.com/bmatcuk/doublestar) globs that are excluded from scanning. Everything else under the scanned paths is searched for annotations.                          |
| `global.output`    | Default output detail (`text`, `wide`, or `github`) shared by `run` and `lint`                                                                                                           |
| `run.verify`       | Verify secure pins against their upstream tags by default (implies a deep lookup)                                                                                                        |
| `run.prerelease`   | Allow selecting prerelease versions by default                                                                                                                                           |
| `run.downgrade`    | Allow selecting versions older than the current one by default                                                                                                                           |
| `run.deep`         | Follow pagination to fetch every version by default (more accurate, but slower)                                                                                                          |
| `run.cache`        | Persist cacheable HTTP responses across runs under the XDG cache directory and revalidate them with conditional requests. On by default - set `false` to fetch everything fresh each run |
| `run.output`       | Output detail for `clover run`; overrides `global.output`                                                                                                                                |
| `lint.output`      | Output detail for `clover lint`; overrides `global.output`                                                                                                                               |
| `fmt.prune`        | Remove unknown directive keys instead of erroring on them by default                                                                                                                     |
| `annotate.write`   | Apply proposed annotations by default instead of previewing them                                                                                                                         |
| `annotate.check`   | Report proposed annotations and exit non-zero by default instead of writing                                                                                                              |

**Precedence**, highest first: an explicit CLI flag, then the per-command key, then `global`, then the built-in default. For the per-marker toggles (`verify`, `prerelease`, `downgrade`), a CLI flag wins over both the config and the directive; otherwise the config supplies the default a directive can still override.

`annotate.write` and `annotate.check` are mutually exclusive.

An unknown key is reported as a warning (with a "did you mean?" hint for a likely typo) and otherwise ignored, so a config written for a newer Clover still loads on an older one. Values are validated against the schema and a malformed one is rejected.

## Environment variables

Clover also reads process-wide settings from `CLOVER_*` environment variables. Set `CLOVER_NO_CACHE=1` to disable the cross-run HTTP cache for a run, overriding `run.cache`.

To make file and line hyperlinks open in an editor, set `CLOVER_HYPERLINK_FORMAT` to one of the supported editor presets:

```bash
export CLOVER_HYPERLINK_FORMAT=vscode
```

For fish:

```fish
set -gx CLOVER_HYPERLINK_FORMAT vscode
```

Supported presets include `cursor`, `kitty`, `macvim`, `subl`, `textmate`, `vscode`, `vscode-insiders`, and `vscodium`. Individual format variables such as `CLOVER_HYPERLINK_LINE_FORMAT` can override a single hyperlink shape when a preset is not specific enough.

## Schema

The `# yaml-language-server` comment on the first line wires the file up to its [JSON schema](https://raw.githubusercontent.com/gechr/clover/main/internal/config/schema.json), so editors with the YAML language server give you completion and validation as you type.

## Selecting the config

By default Clover loads the user config and overlays the nearest `.clover.yaml`. Override that per run:

```bash
# replace the project config with an explicit file (the user layer still applies)
clover run --config path/to/.clover.yaml

# ignore both layers for a fully unconfigured run
clover run --no-config
```

Run [`clover init`](commands.md) to create a starter config interactively.
