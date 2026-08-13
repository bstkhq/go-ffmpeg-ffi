# go-ffmpeg-ffi documentation

This directory is the canonical home for project architecture and planning.
Implementation detail belongs in code, tests, issues, and pull requests rather
than in additional plan documents.

## Canonical documents

- [Architecture](architecture.md): support policy, runtime layers, ABI rules,
  C shim boundaries, ownership, and initialization.
- [Roadmap](roadmap.md): the single implementation plan, covering the six
  bootstrap pull requests and the Android-first platform rollout through the
  Windows/macOS desktop closure, with explicit compile, integration, and
  hardware-qualification gates.

## Inherited reference documents

The following documents came from ffgo. They are retained while useful content
is migrated, but their claims are not go-ffmpeg-ffi guarantees:

- [Original internal design](internal-design.md)
- [Original user guide](user-guide.md)
- [Original feature gap analysis](gap-analysis.md)

The canonical set is intentionally small. New cross-cutting decisions should
update `architecture.md`; schedule or priority changes should update
`roadmap.md`.
