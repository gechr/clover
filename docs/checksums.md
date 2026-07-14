# Checksums & digests

Besides the version itself, an annotation can render a **side value** computed for that version, such as a checksum, a digest, or a commit. Set `value` to choose which.

| `value`   | Renders                                         |
| --------- | ----------------------------------------------- |
| `version` | The resolved version (the default)              |
| `commit`  | The commit a tag or branch resolves to (GitHub) |
| `sha256`  | A `sha256:` digest or asset checksum            |

```yaml
# clover: provider=github repository=cli/cli id=gh constraint=minor
version: 2.40.0

# clover: from=gh value=sha256 pattern=*_linux_amd64.tar.gz
checksum: 17f3c21f3f4c3b0175a9a0ee8f8e42e36f58e2713de81440ea9c0cb94c5a08a8
```

A side value is only refreshed when its version actually changed, so a checksum never drifts out of step with the version it belongs to. This is also a safety guarantee: a digest that was once pinned never moves on its own, so a re-published artifact cannot silently change a checksum under an unchanged version. Pass `--force` (or set `run.force` in [`.clover.yaml`](configuration.md)) to deliberately re-pin every followed digest against its current version, for the rare case where an upstream release was legitimately re-published.

## Sourcing a sha256

`sha256-source` controls how the checksum is obtained:

| Source      | Behavior                                                           |
| ----------- | ------------------------------------------------------------------ |
| `auto`      | Digest, then a checksums file, then download (the default)         |
| `digest`    | Use the provider's asset digest, no download                       |
| `checksums` | Read a published checksums file (`sha256-url`, or a sibling asset) |
| `download`  | Download the asset and hash it                                     |
| `verify`    | Require the digest and the checksums file to agree                 |

Two keys refine `checksums`/`download`:

- `sha256-url` is the checksum-file URL, templated with `<version>`.
- `pattern` is an asset filename glob selecting which asset to hash.

On a private GitHub, GitLab, or Gitea repository, assets and sibling checksum files are read through the authenticated API rather than the public download URL, so `checksums` and `download` work wherever discovery does.
