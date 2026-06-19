# Providers

A provider is the source of truth a directive resolves against. Set it with the `provider` key, or omit it and let Clover [infer one](auto.md) from the target line.

| Provider | Tracks                                              | Page                      |
| -------- | --------------------------------------------------- | ------------------------- |
| `github` | Releases, tags, and branch commits of a repository. | [GitHub](github.md)       |
| `docker` | Image tags and digests from a container registry.   | [Docker](docker.md)       |
| `auto`   | Inferred from the target line's contents.           | [Auto-detection](auto.md) |

A directive with no `provider` and a `from` key is a **follower** - it reads a value resolved elsewhere instead of contacting an upstream. See [Following values](following.md).
