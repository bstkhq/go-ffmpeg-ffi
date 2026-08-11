# Contributing to go-ffmpeg-ffi

Thank you for helping build go-ffmpeg-ffi. This is a hard fork of
[ffgo](https://github.com/obinnaokechukwu/ffgo); please preserve the original
authors' attribution when moving or modifying inherited work.

The source tree remains transitional until the first release. See the
[architecture](docs/architecture.md) for technical decisions and the
[roadmap](docs/roadmap.md) for the six bootstrap PRs.

## Before opening a change

- Search existing issues and PRs.
- Use a focused branch and keep changes inside one roadmap subsystem.
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
go vet -unsafeptr=false ./...
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

The `unsafeptr` analyzer is disabled because this FFI layer intentionally
converts native addresses. Those conversions require ABI-specific review and
integration or stress coverage instead of a blanket analyzer exemption.

FFmpeg-facing changes must also run the relevant pinned integration job from the
[test matrix](docs/roadmap.md#pr-5-make-compatibility-claims-executable). ABI,
ownership, callbacks, or native cleanup changes require the corresponding
stress or sanitizer coverage. PR descriptions must report exactly what was run;
“all versions” is not sufficient.

## Commits and pull requests

Use clear imperative commit subjects and explain why unsafe or ABI-sensitive
code is correct. A bootstrap PR may contain several logical commits, but its
scope must remain one of the six roadmap units.

Development may be assisted by OpenAI Codex. Human contributors remain the
authors, reviewers, and accountable maintainers. When Codex materially helped a
commit, add this trailer after the commit message body:

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
