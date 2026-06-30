# Ignoring files & lines

Sometimes a `clover:` directive shouldn't run, or a whole file or tree should stay untouched. Clover offers two mechanisms: **ignore control comments** for individual directives, blocks, and files, and **`.gitignore`** for pruning paths during the scan.

## Ignore control comments

These are standalone `clover:` comments - not keys inside a directive - written in the host file's comment syntax. Only the first token is matched, so a trailing explanation is fine (`# clover:ignore-file generated, do not edit`).

### Ignore the next directive

`clover:ignore` suppresses the directive on the line immediately below it - place it directly above the `clover:` comment you want to disable:

```yaml
# clover:ignore
# clover: provider=github repository=ignored/one
x: 1
# clover: provider=github repository=kept/two
y: 2
```

Only `ignored/one` is skipped; `kept/two` is processed as usual.

### Ignore a block

`clover:ignore-start` and `clover:ignore-end` bracket a region; every directive between them is suppressed:

```yaml
# clover:ignore-start
# clover: provider=github repository=block/one
# clover: provider=github repository=block/two
# clover:ignore-end
# clover: provider=github repository=kept/three
z: 3
```

### Ignore a whole file

`clover:ignore-file` drops the entire file from the scan, wherever it appears:

```yaml
# clover:ignore-file

# clover: provider=github repository=whole/file
w: 4
```

### `clover:ignore` vs `disabled`

The [`disabled`](annotations.md#disabling-and-filtering) key disables a single annotation from within the annotation itself. The ignore comments are separate controls, and they reach further - a block, a whole file, or (with `.gitignore`) entire trees - which a per-annotation key cannot.

## `.gitignore`

During the walk Clover prunes any path your `.gitignore` files match, so generated output, vendored code, and build directories are never scanned. The matching follows git's own rules: gitignore syntax, files applied from the repository root down to each directory, last match winning, and negation (`!pattern`) restoring a path. Files outside any repository are never ignored, and the VCS directories themselves (`.git`, `.jj`, `.hg`, `.svn`) are always skipped.

```text
# .gitignore
ignored/
```

A directive in `ignored/` is pruned; a sibling outside it is scanned normally.

## Overriding with `--no-ignore`

Pass `--no-ignore` to `run`, `lint`, or `format` to scan files that `.gitignore` would otherwise exclude. VCS directories (`.git`, `.jj`, `.hg`, `.svn`) stay excluded regardless, and the in-file `clover:ignore` controls still apply - it only disables `.gitignore` pruning.

```text
clover run --no-ignore
```

For excludes that live with the project config rather than in `.gitignore`, see [`paths.exclude`](configuration.md).
