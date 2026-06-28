# HTTP

The HTTP provider fetches an arbitrary URL and extracts version candidates from the response - the escape hatch for any source Clover has no bespoke provider for. It performs one anonymous `GET` of `url` and pulls one-or-many version strings out of the body, then lets the usual [constraint](constraints.md) and [filtering](filtering.md) keys select among them.

How the body is read is set by the extraction key, and exactly one is required:

- `jq` runs a [jq](https://jqlang.org/) program over a JSON body.
- `extract` matches a glob (with a `<version>` placeholder) or a `/regex/` over a text body.

```yaml
# clover: provider=http url=https://api.github.com/repos/cli/cli/releases jq='.[].tag_name' constraint=minor
gh_version: 2.62.0
```

## Keys

| Key                                   | Description                                                                                  |
| ------------------------------------- | -------------------------------------------------------------------------------------------- |
| `provider`                            | `http`.                                                                                      |
| `url`                                 | The endpoint to fetch. Must be `http` or `https`. Required.                                  |
| `jq`                                  | A jq program over a JSON response body. Each string it yields is a candidate.                |
| `extract`                             | A glob with `<version>` or a `/regex/` over a text response body. Each match is a candidate. |
| `user-agent`                          | The `User-Agent` header to send. Defaults to `Clover v<version>`.                            |
| [`constraint`](constraints.md)        | How far the version may move (`major`/`minor`/`patch`, or a semver range).                   |
| [`include` / `exclude`](filtering.md) | Filter the candidate versions.                                                               |
| [`cooldown`](cooldown.md)             | Require a minimum age before a version is eligible.                                          |

Set exactly one of `jq` or `extract`. The expression is compiled when the directive is validated, so [`clover lint`](commands.md) reports a malformed `jq` program or `extract` pattern offline, before any request is made.

The endpoint is fetched anonymously, so the HTTP provider needs no authentication; the request carries an identifying `User-Agent` (`Clover v<version>`) that `user-agent` overrides. It is selected explicitly with `provider=http` - a bare version line carries no signal to [infer](auto.md) it from.

## Extracting with `jq`

`jq` parses the response as JSON and runs the program against it. A program that emits a stream - one value per result, the `.[].tag_name` idiom - contributes each string it yields; a program that returns a single array contributes each string element. Non-string results are ignored, and repeated versions are surfaced once in first-seen order.

```yaml
# A JSON array of release objects: take every tag.
# clover: provider=http url=https://api.example.com/releases jq='.[].tag_name' constraint=minor
app_version: 1.4.2

# A single object with one field: take that value.
# clover: provider=http url=https://example.com/version.json jq='.latest'
app_version: 1.4.2
```

## Extracting with `extract`

`extract` reads the body as text and matches the [find pattern](find-replace.md) grammar against it: a glob whose `<version>` placeholder captures the version, or a `/regex/` whose first capture group (or whole match, when it has none) is the version. Every match across the body becomes a candidate. A glob must carry a single `<version>` token - the component tokens (`<major>`/`<minor>`/`<patch>`) would each capture only a fragment, so they are rejected; reach for a `/regex/` when you need finer control.

```yaml
# A plain-text "latest" file.
# clover: provider=http url=https://example.com/latest extract=/v(\d+\.\d+\.\d+)/
app_version: 1.4.2

# A page listing artifacts: capture each version with a glob token.
# clover: provider=http url=https://example.com/dist/ extract=app-<version>-linux-x64.tar.gz constraint=minor
app_version: 1.4.2
```

A single fetch returns whatever the endpoint serves, so Clover always sees every version the response carries - `--deep` has nothing extra to fetch.

## Checksums

When the source also publishes a checksums file, a [follower](checksums.md) can keep a checksum in lockstep with the version by templating [`sha256-url`](checksums.md#sourcing-a-sha256) with `<version>` and selecting the artifact with `pattern`:

```yaml
# clover: provider=http id=app url=https://api.example.com/releases jq='.[].tag_name' constraint=minor
app_version: 1.4.2

# clover: from=app value=sha256 sha256-url=https://example.com/app/v<version>/SHA256SUMS pattern=app-<version>-linux-x64.tar.gz
app_sha256: 0000000000000000000000000000000000000000000000000000000000000000
```
