//go:build amd64 || arm64

package dynlib

// ProcessImage identifies the already-linked process image. iOS applications
// can link FFmpeg statically, or load signed FFmpeg frameworks before ffgo is
// initialized; both expose their symbols through the process-wide namespace.
const ProcessImage = "@ffgo/process-image"
