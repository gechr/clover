# Auto-detection

When you omit `provider` (or set `provider=auto`), Clover infers the provider and its parameters from the content of the target line. Common cases need almost no annotation.

```dockerfile
# clover: constraint=minor
FROM redis:7.2.0
```

Here Clover recognizes a Docker image reference on the line and resolves it with the [Docker](docker.md) provider, inferring `repository=redis`. A line that names a GitHub repository resolves with the [GitHub](github.md) provider instead.

## When to be explicit

Auto-detection covers the obvious cases. Set `provider` explicitly when:

- the line is ambiguous, or the value you want to track is not the most obvious token on it,
- you are tracking something that is not literally written on the target line,
- you want the annotation to document intent regardless of the line's contents.

Inference only fills in what you leave out - any key you set yourself always wins.

## Generating annotations

To add `provider=auto` directives across an existing codebase rather than write them by hand, run [`clover annotate`](commands.md#annotate). It scans for the same lines auto-detection recognizes and inserts a directive above each, so onboarding a repository is a single command.
