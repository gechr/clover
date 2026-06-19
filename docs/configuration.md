# Configuration

Most of Clover's behavior lives in the annotations themselves, so configuration is deliberately small. A project can add a `.clover.yaml` (or `.clover.yml`) file at its root to set a couple of project-wide options.

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/gechr/clover/main/internal/config/schema.json

required-version: ">=0.1.0"

paths:
  exclude:
    - vendor/**
    - "**/testdata/**"
```

## Options

| Key                | Description                                                                                                                                                     |
| ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `required-version` | A version constraint the running `clover` binary must satisfy (e.g. `">=0.1.0"`, `"~>0.1"`). Clover refuses to run if its own version falls outside the range.  |
| `paths.exclude`    | [Doublestar](https://github.com/bmatcuk/doublestar) globs that are excluded from scanning. Everything else under the scanned paths is searched for annotations. |

## Schema

The `# yaml-language-server` comment on the first line wires the file up to its [JSON schema](https://raw.githubusercontent.com/gechr/clover/main/internal/config/schema.json), so editors with the YAML language server give you completion and validation as you type.

## Selecting the config

By default Clover loads the nearest `.clover.yaml`. Override it per run:

```bash
# use an explicit config file
clover run --config path/to/.clover.yaml

# ignore any config file
clover run --no-config
```

Run [`clover init`](commands.md) to create a starter config interactively.
