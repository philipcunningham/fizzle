package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyFileRejectsMissingAndCorruptArchives(t *testing.T) {
	if err := verifyFile(filepath.Join(t.TempDir(), "missing"), "00"); err == nil {
		t.Fatal("missing archive accepted")
	}
	path := filepath.Join(t.TempDir(), "corrupt")
	if err := os.WriteFile(path, []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, "00"); err == nil {
		t.Fatal("corrupt archive accepted")
	}
}

func TestVerifiedArchiveExtracts(t *testing.T) {
	archive := testArchive(t, "corpus/example/file.fzf", []byte("fixture"))
	path := filepath.Join(t.TempDir(), "corpus.tar.gz")
	if err := os.WriteFile(path, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	if err := verifyFile(path, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := extractFile(path, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "corpus", "example", "file.fzf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fixture" {
		t.Fatalf("content = %q", got)
	}
}

func TestCorpusReadyRequiresMatchingVersionMarker(t *testing.T) {
	root := t.TempDir()
	if corpusReady(root, "wanted") {
		t.Fatal("missing marker accepted")
	}
	if err := os.WriteFile(filepath.Join(root, ".archive-sha256"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if corpusReady(root, "wanted") {
		t.Fatal("wrong marker accepted")
	}
	if err := os.WriteFile(filepath.Join(root, ".archive-sha256"), []byte("wanted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !corpusReady(root, "wanted") {
		t.Fatal("matching marker rejected")
	}
}

func TestArchiveCannotEscapeDestination(t *testing.T) {
	archive := testArchive(t, "../escape", []byte("bad"))
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if err := extractTar(tar.NewReader(gz), t.TempDir()); err == nil {
		t.Fatal("escaping archive path accepted")
	}
}

func testArchive(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
