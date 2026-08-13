package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/shim"
)

func TestVerifyManifest(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "libffshim.so")
	if err := os.WriteFile(binary, []byte("attested shim"), 0o755); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(binary)
	if err != nil {
		t.Fatal(err)
	}
	m := manifest{
		Schema:     manifestSchema,
		Platform:   "linux/amd64",
		File:       filepath.Base(binary),
		SHA256:     digest,
		ShimAPI:    shim.APIVersion,
		ContractID: fmt.Sprintf("0x%016x", shim.ContractID),
		FFmpeg:     ffmpeg{Major: 9},
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(manifestPath); err != nil {
		t.Fatalf("verify valid manifest: %v", err)
	}

	m.ContractID = "0x0000000000000000"
	encoded, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(manifestPath); err == nil {
		t.Fatal("verify accepted a manifest with a stale contract")
	}

	m.ContractID = fmt.Sprintf("0x%016x", shim.ContractID)
	m.File = "../outside-the-artifact"
	encoded, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(manifestPath); err == nil {
		t.Fatal("verify accepted a manifest file path outside its artifact directory")
	}
}
