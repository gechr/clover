# Cooldown

`cooldown` requires a version to be at least a certain age before Clover will adopt it. A fresh release is held back until it has had time to settle, which guards against yanked or quickly-patched versions.

```dockerfile
# clover: provider=github repository=redis/redis cooldown=72h constraint=minor
FROM redis:7.2.0
```

## Durations

The value is a duration. Hours work, and longer units compose:

| Value  | Meaning            |
| ------ | ------------------ |
| `72h`  | 72 hours           |
| `2w3d` | 2 weeks and 3 days |

A version that is newer than its cooldown is simply skipped; Clover stays on the current value until the candidate ages in.

Cooldown still applies when [tracking floating refs](tracking.md) - a digest or commit that is too fresh is held back even though no version is being selected.
