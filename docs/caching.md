# Caching

Clover caches upstream HTTP responses at two tiers, in memory for the current run and on disk across runs, so repeated lookups cost one network round trip instead of hundreds. Providers are unaware of it. The cache is a transport beneath their ordinary HTTP clients, which is also the most effective way to stay inside an API's rate limit.

## In-run caching

Within a single run, every cacheable response is memoized in memory:

- **Memoization.** A hundred markers pointing at the same upstream (the same GitHub repository, the same Docker tag list) cause a single request. Every later lookup is served from memory without a network call.
- **Coalescing.** Concurrent identical requests share one in-flight round trip instead of racing each other to the same origin.
- **Negative caching.** A stable client error, such as a 404 for a missing release, is remembered too, so repeated probes don't re-ask a question whose answer can't change mid-run. Retryable statuses (408, 429, 5xx) are never cached, since a later attempt could clear them.
- **Error backoff.** A failed fetch, such as an unreachable host, is replayed to further callers for a short window, so a dead host costs one connection timeout rather than one per marker.

Entries are keyed by method, URL, and headers that change the response. The `Authorization` header is part of the key (hashed, never stored raw), so authenticated and anonymous responses never mix. Responses larger than 16 MiB, and anything the origin marks `Cache-Control: no-store`, bypass the cache.

The in-run tier is always on. It holds for the lifetime of one command and needs no cleanup.

## Cross-run caching

`clover run` additionally persists cacheable responses under the XDG cache directory (`~/.cache/clover/http` on Linux) and reuses them on later runs:

- **Freshness.** An entry is served straight from disk while inside the freshness lifetime its origin granted (`Cache-Control: max-age` or `Expires`).
- **Revalidation.** A stale entry carrying a validator (`ETag` or `Last-Modified`) is revalidated with a conditional request. When the origin answers `304 Not Modified`, the common case for registry and API responses, Clover reuses the stored body and pays for a header exchange instead of a full download. GitHub GraphQL responses carry no validators, so they get a short fallback freshness window of one minute instead.
- **Hygiene.** Credentials never reach disk. Sensitive headers (`Authorization`, `Set-Cookie`, and the like) are stripped before an entry is persisted, and the files are written with owner-only permissions. Entries unused for 30 days are garbage-collected.

The disk tier is best-effort. If the cache directory can't be opened, Clover warns and runs with the in-memory tier alone, so a broken cache never breaks a run.

## Turning it off

The cross-run cache is on by default. Three switches disable it, each level winning over the next:

1. The `--[no-]cache` flag on `clover run` (see [Commands](commands.md)).
2. The `CLOVER_NO_CACHE` environment variable, where any non-empty value disables the cache.
3. The [`run.cache`](configuration.md) config key, which fetches everything fresh each run when set to `false`.

`--no-cache` disables only the *cross-run* disk tier. In-run memoization and coalescing still apply, so a single run never re-fetches the same URL twice either way.

## Observing it

Run with `--verbose` and the end-of-run report includes a transport-activity line: how many lookups reached the network, how many were cache hits, how many revalidated with a `304`, how many coalesced into a shared request, and how many failures the backoff replayed.
