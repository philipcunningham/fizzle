package architecture_test

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationPackagesDoNotDependOnRetiredCommandPackages(t *testing.T) {
	root := repositoryRoot(t)
	for _, dir := range []string{"pkg/webcore", "pkg/document", "pkg/fzf"} {
		inspectGoFiles(t, filepath.Join(root, dir), func(path string, file *ast.File) {
			for _, spec := range file.Imports {
				name, _ := strconv.Unquote(spec.Path.Value)
				for _, retired := range []string{"/pkg/diskadd", "/pkg/diskget", "/pkg/disklist"} {
					if strings.HasSuffix(name, retired) {
						t.Errorf("%s imports retired command package %s", relative(root, path), name)
					}
				}
			}
		})
	}
}

func TestFormatProjectionUsesCanonicalViews(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"pkg/fzfinfo/fzfinfo.go", "pkg/webcore/instrument.go"} {
		path := filepath.Join(root, name)
		file := parseFile(t, path)
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if imported == "encoding/binary" {
				t.Errorf("%s parses raw format bytes instead of using fzf views", name)
			}
		}
	}
}

func TestRetiredRawLayoutAPIsStayPrivate(t *testing.T) {
	root := repositoryRoot(t)
	retired := map[string]bool{
		"CountBankSectors": true,
		"CountAllVoices":   true,
		"InferVoiceCount":  true,
	}
	inspectGoFiles(t, filepath.Join(root, "pkg"), func(path string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && retired[selector.Sel.Name] {
				t.Errorf("%s calls retired raw layout API %s", relative(root, path), selector.Sel.Name)
			}
			return true
		})
	})
}

func TestSessionFacadeStaysFocused(t *testing.T) {
	path := filepath.Join(repositoryRoot(t), "pkg/webcore/session.go")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close() //nolint:errcheck
	lines := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines > 500 {
		t.Fatalf("webcore/session.go has %d lines; move boundary, projection, or history responsibilities out before exceeding 500", lines)
	}
}

func inspectGoFiles(t *testing.T, root string, inspect func(string, *ast.File)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		inspect(path, parseFile(t, path))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func parseFile(t *testing.T, path string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("architecture test source path unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
}

func relative(root, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return name
}
