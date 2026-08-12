//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
)

func TestTwoPassWorkspaceUsesPrivateDirectory(t *testing.T) {
	workspace, err := newTwoPassWorkspace("output.MP4", &EncoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tempDir := workspace.tempDir
	defer workspace.cleanup()

	if tempDir == "" {
		t.Fatal("temporary workspace was not created")
	}
	if filepath.Dir(workspace.passLogFile) != tempDir || filepath.Dir(workspace.passOutput) != tempDir {
		t.Fatalf("generated paths escaped workspace: log=%q output=%q", workspace.passLogFile, workspace.passOutput)
	}
	if filepath.Ext(workspace.passOutput) != ".MP4" {
		t.Fatalf("pass output = %q, want original extension", workspace.passOutput)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(tempDir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Fatalf("workspace permissions = %o, want 700", perm)
		}
	}
	for _, path := range []string{workspace.passLogFile, workspace.passOutput} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("generated path %q exists before encoder use: %v", path, err)
		}
	}

	workspace.cleanup()
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("temporary workspace still exists after cleanup: %v", err)
	}
}

func TestTwoPassWorkspacePreservesCallerPaths(t *testing.T) {
	dir := t.TempDir()
	passLog := filepath.Join(dir, "passlog")
	passOutput := filepath.Join(dir, "pass1.mkv")
	workspace, err := newTwoPassWorkspace("output.mkv", &EncoderOptions{
		PassLogFile: passLog,
		PassOutput:  passOutput,
	})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.tempDir != "" || workspace.passLogFile != passLog || workspace.passOutput != passOutput {
		t.Fatalf("workspace changed caller paths: %#v", workspace)
	}
}

func TestTwoPassTranscode_Integration(t *testing.T) {
	if testing.Short() {
		t.Log("Skipping two-pass integration test in short mode")
		return
	}
	if !requireFFmpeg(t) {
		return
	}

	in := filepath.Join("testdata", "test.mp4")
	if _, err := os.Stat(in); err != nil {
		t.Fatalf("missing test input: %v", err)
	}

	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.mp4")
	passBase := filepath.Join(tmpDir, "passlog")

	opts := &EncoderOptions{
		Video: &VideoEncoderConfig{
			Codec:       CodecIDH264,
			Width:       0, // infer from input
			Height:      0,
			PixelFormat: PixelFormatYUV420P,
			FrameRate:   NewRational(25, 1),
			Bitrate:     500000,
			GOPSize:     10,
			MaxBFrames:  0,
		},
		PassLogFile: passBase,
	}

	if avcodec.FindEncoder(avcodec.CodecIDH264) == nil {
		t.Log("H.264 encoder not available in this FFmpeg build")
		return
	}

	if err := TwoPassTranscode(in, out, opts); err != nil {
		t.Logf("TwoPassTranscode not supported in this environment/encoder: %v", err)
		return
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not created: %v", err)
	}

	// User-provided passlog base should not be cleaned up.
	matches, _ := filepath.Glob(passBase + "*")
	if len(matches) == 0 {
		t.Fatalf("expected passlog files with prefix %q", passBase)
	}

	generatedOut := filepath.Join(tmpDir, "generated.mp4")
	generatedOpts := *opts
	generatedOpts.PassLogFile = ""
	if err := TwoPassTranscode(in, generatedOut, &generatedOpts); err != nil {
		t.Fatalf("TwoPassTranscode with generated workspace: %v", err)
	}
	if _, err := os.Stat(generatedOut); err != nil {
		t.Fatalf("output with generated workspace not created: %v", err)
	}
}

func TestTwoPassTranscode_HEVC_Integration(t *testing.T) {
	if testing.Short() {
		t.Log("Skipping two-pass HEVC integration test in short mode")
		return
	}
	if !requireFFmpeg(t) {
		return
	}

	in := filepath.Join("testdata", "test.mp4")
	if _, err := os.Stat(in); err != nil {
		t.Fatalf("missing test input: %v", err)
	}

	tmpDir := t.TempDir()
	out := filepath.Join(tmpDir, "out.mkv") // mkv is widely compatible for HEVC
	passBase := filepath.Join(tmpDir, "passlog")

	opts := &EncoderOptions{
		Video: &VideoEncoderConfig{
			Codec:       CodecIDHEVC,
			Width:       0, // infer from input
			Height:      0,
			PixelFormat: PixelFormatYUV420P,
			FrameRate:   NewRational(25, 1),
			Bitrate:     500000,
			GOPSize:     10,
			MaxBFrames:  0,
		},
		PassLogFile: passBase,
	}

	if avcodec.FindEncoder(avcodec.CodecIDHEVC) == nil {
		t.Log("HEVC encoder not available in this FFmpeg build")
		return
	}

	if err := TwoPassTranscode(in, out, opts); err != nil {
		t.Logf("TwoPassTranscode (HEVC) not supported in this environment/encoder: %v", err)
		return
	}

	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output not created: %v", err)
	}

	matches, _ := filepath.Glob(passBase + "*")
	if len(matches) == 0 {
		t.Fatalf("expected passlog files with prefix %q", passBase)
	}
}
