package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestContractIDIgnoresCheckoutLineEndings(t *testing.T) {
	t.Parallel()

	lfRoot := t.TempDir()
	crlfRoot := t.TempDir()
	files := map[string][]byte{
		"shim/ffshim.c":         []byte("line one\nline two\n"),
		"shim/ffshim.h":         []byte("header one\nheader two\n"),
		"internal/shim/shim.go": []byte("package shim\n"),
	}
	for name, contents := range files {
		writeFixture(t, lfRoot, name, contents)
		writeFixture(t, crlfRoot, name, bytes.ReplaceAll(contents, []byte("\n"), []byte("\r\n")))
	}

	lfID, err := contractID(lfRoot)
	if err != nil {
		t.Fatalf("contractID with LF: %v", err)
	}
	crlfID, err := contractID(crlfRoot)
	if err != nil {
		t.Fatalf("contractID with CRLF: %v", err)
	}
	if lfID != crlfID {
		t.Fatalf("contract IDs differ by checkout line endings: LF=%#016x CRLF=%#016x", lfID, crlfID)
	}
}

func TestGeneratedFileMatchesAcceptsCRLF(t *testing.T) {
	t.Parallel()

	want := []byte("first\nsecond\n")
	got := []byte("first\r\nsecond\r\n")
	if !generatedFileMatches(got, want) {
		t.Fatal("generated file with CRLF should match LF output")
	}
}

func writeFixture(t *testing.T, root, name string, contents []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
