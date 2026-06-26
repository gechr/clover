# Prereleases

By default Clover ignores prerelease versions (`1.5.0-rc1`, `2.0.0-beta`, and similar) and selects only stable releases.

## Allowing prereleases

Opt in per annotation with `prerelease`:

```dockerfile
# clover: provider=github repository=redis/redis prerelease constraint=minor
FROM redis:7.2.0
```

or for a whole run:

```bash
clover run --prerelease
```

## Forcing them off

If the current line already sits on a prerelease but you want Clover to move it back onto the stable track, set `prerelease=false` (or run with `--no-prerelease`) to exclude prereleases explicitly.

For filtering prereleases by name rather than by their semver tag, use [`include` / `exclude`](filtering.md).
