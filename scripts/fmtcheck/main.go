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
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error { //nolint:gosec // The explicit CLI root is intentionally traversed.
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		source, err := os.ReadFile(path) //nolint:gosec // path comes from the bounded repository walk above.
		if err != nil {
			return err
		}
		// Git may materialise tracked LF files as CRLF on Windows. Formatting
		// is a property of the Go tokens, not the checkout's line-ending policy,
		// so compare canonical LF bytes while leaving the working tree untouched.
		canonical := bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
		formatted, err := format.Source(canonical)
		if err != nil {
			return fmt.Errorf("formatting %s: %w", path, err)
		}
		if !bytes.Equal(canonical, formatted) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
