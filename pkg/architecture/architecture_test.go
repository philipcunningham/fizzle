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
	graph := packageGraph(t, root)
	for _, packageName := range []string{"github.com/philipcunningham/fizzle/pkg/webcore", "github.com/philipcunningham/fizzle/pkg/document", "github.com/philipcunningham/fizzle/pkg/fzf"} {
		if path := dependencyPath(graph, packageName, map[string]bool{
			"github.com/philipcunningham/fizzle/pkg/diskadd":  true,
			"github.com/philipcunningham/fizzle/pkg/diskget":  true,
			"github.com/philipcunningham/fizzle/pkg/disklist": true,
		}); len(path) > 0 {
			t.Errorf("retired command dependency survives transitively: %s", strings.Join(path, " -> "))
		}
	}
}

func TestDependencyGuardRejectsBridgePackages(t *testing.T) {
	graph := map[string][]string{"webcore": {"bridge"}, "bridge": {"disklist"}}
	if path := dependencyPath(graph, "webcore", map[string]bool{"disklist": true}); len(path) == 0 {
		t.Fatal("transitive command dependency was accepted")
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
		ast.Inspect(file, func(node ast.Node) bool {
			if hasLiteralOffset(node) {
				t.Errorf("%s contains a literal byte offset instead of a bounded view", name)
			}
			return true
		})
	}
}

func TestProjectionGuardRejectsLiteralOffsets(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "bad.go", "package bad; func read(b []byte) byte { return b[0x202] }", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool { found = found || hasLiteralOffset(node); return true })
	if !found {
		t.Fatal("literal format offset was accepted")
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

func TestWebcoreFilesStayFocused(t *testing.T) {
	root := filepath.Join(repositoryRoot(t), "pkg/webcore")
	inspectGoFiles(t, root, func(path string, _ *ast.File) {
		if lines := lineCount(t, path); lines > 500 {
			t.Errorf("%s has %d lines; keep each boundary responsibility below 500", filepath.Base(path), lines)
		}
	})
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
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
	return lines
}

func hasLiteralOffset(node ast.Node) bool {
	switch value := node.(type) {
	case *ast.IndexExpr:
		_, ok := value.Index.(*ast.BasicLit)
		return ok
	case *ast.SliceExpr:
		_, low := value.Low.(*ast.BasicLit)
		_, high := value.High.(*ast.BasicLit)
		return low || high
	default:
		return false
	}
}

func packageGraph(t *testing.T, root string) map[string][]string {
	t.Helper()
	const module = "github.com/philipcunningham/fizzle/"
	graph := make(map[string][]string)
	inspectGoFiles(t, filepath.Join(root, "pkg"), func(path string, file *ast.File) {
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(module, "/") + "/" + filepath.ToSlash(rel)
		for _, spec := range file.Imports {
			imported, _ := strconv.Unquote(spec.Path.Value)
			if strings.HasPrefix(imported, module) {
				graph[name] = append(graph[name], imported)
			}
		}
	})
	return graph
}

func dependencyPath(graph map[string][]string, start string, forbidden map[string]bool) []string {
	type route []string
	queue := []route{{start}}
	seen := map[string]bool{start: true}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		for _, next := range graph[path[len(path)-1]] {
			candidate := append(append(route{}, path...), next)
			if forbidden[next] {
				return candidate
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, candidate)
			}
		}
	}
	return nil
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
