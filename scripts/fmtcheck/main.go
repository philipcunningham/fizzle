// Command fmtcheck reports Go source files that gofmt would change without
// modifying them.
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	files, err := unformatted(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, path := range files {
		fmt.Println(path)
	}
	if len(files) > 0 {
		fmt.Fprintln(os.Stderr, "Go files need formatting; run make fmt")
		os.Exit(1)
	}
}

func unformatted(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(source)
		if err != nil {
			return fmt.Errorf("formatting %s: %w", path, err)
		}
		if !bytes.Equal(source, formatted) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
