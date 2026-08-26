package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnformattedReportsWithoutChangingSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	source := []byte("package example\nfunc bad( ){ }\n")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := unformatted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != path {
		t.Fatalf("unformatted = %v, want [%s]", files, path)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(source) {
		t.Fatalf("fmtcheck changed source to %q", after)
	}
}

func TestUnformattedIgnoresDependencyTrees(t *testing.T) {
	dir := t.TempDir()
	dependency := filepath.Join(dir, "node_modules", "dependency")
	if err := os.MkdirAll(dependency, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "bad.go"), []byte("package dependency\nfunc bad( ){ }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := unformatted(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("unformatted dependency files = %v, want none", files)
	}
}
