# Contributing to go-ffmpeg-ffi

Thank you for helping build go-ffmpeg-ffi. This is a hard fork of
[ffgo](https://github.com/obinnaokechukwu/ffgo); please preserve the original
authors' attribution when moving or modifying inherited work.

The API continues to evolve and is not compatible with the original ffgo API.
See the [architecture](docs/architecture.md) for technical decisions, the
[support matrix](docs/support.md) for current evidence, and the
[roadmap](docs/roadmap.md) for remaining qualification work.

## Before opening a change

- Search existing issues and PRs.
- Use a focused branch and keep each PR within one coherent subsystem.
- State the operating system, architecture, Go version, complete FFmpeg library
  versions, shim status, and reproduction steps for bugs.
- Prefer small redistributable media fixtures; include sound when testing the
  go-avebi playback path.
- Do not present inherited features as verified until the compatibility matrix
  proves them.

## Code rules

- Format Go code with `gofmt` and keep exported APIs documented.
- Return contextual errors instead of panicking on runtime or media failures.
- Make native ownership explicit and pair every allocation with deterministic
  cleanup.
- Never retain an ordinary Go heap pointer in native code. Use integer callback
  handles and keep callbacks panic-safe.
- Keep all version-specific layouts in `internal/abi`; do not add local magic
  offsets.
- Treat missing optional symbols as capabilities and missing required symbols
  as initialization errors.
- Update the canonical architecture or roadmap instead of creating another plan
  document.

## Test expectations

At minimum, run:

```bash
gofmt -w <changed-go-files>
go vet ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

The `unsafeptr` analyzer remains enabled. Declare native pointer results as
`unsafe.Pointer`, and use `unsafe.Add` when a field address must remain related
to its base pointer. Any unavoidable integer-to-pointer conversion requires
ABI-specific review and integration or stress coverage.

FFmpeg-facing changes must also run the relevant pinned integration job from the
[test matrix](docs/support.md#ffmpeg-versions). ABI,
ownership, callbacks, or native cleanup changes require the corresponding
stress or sanitizer coverage. PR descriptions must report exactly what was run;
“all versions” is not sufficient.

## Commits and pull requests

Use clear imperative commit subjects and explain why unsafe or ABI-sensitive
code is correct. A PR may contain several logical commits, but its scope must
remain coherent and reviewable.

Development uses Agentic Coding with OpenAI Codex, based on the GPT-5 model
family. The agent may inspect and edit the workspace, run builds and tests, and
use configured GitHub and Android integration tools. Human contributors remain
the authors, reviewers, and accountable maintainers: they define scope, approve
privileged actions, review evidence, and decide what is merged. When Codex
materially helped a commit, add this trailer after the commit message body:

```text
Assisted-by: OpenAI Codex
```

Do not invent a Codex email address for `Co-authored-by`. Use that Git trailer
only when a real contributor has supplied a verifiable name and email. PRs
should also disclose material AI assistance and the human validation performed.

## Review checklist

- The PR names affected FFmpeg release lines and platforms.
- ABI, ownership, callback, and compatibility effects are described.
- Tests and exact loaded library/shim versions are recorded.
- Documentation and examples match what is actually verified.
- Inherited attribution and Apache-2.0 notices remain intact.
- Codex assistance, if material, is disclosed.

## License

Contributions are licensed under Apache-2.0. FFmpeg remains separately licensed
under LGPL or GPL according to its build. Do not add prebuilt FFmpeg or shim
artifacts without their corresponding license, source/configuration material,
and distribution review.
