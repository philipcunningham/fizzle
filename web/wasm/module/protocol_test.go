package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type protocolManifest struct {
	Version int `json:"version"`
	Error   struct {
		Envelope string   `json:"envelope"`
		Fields   []string `json:"fields"`
	} `json:"error"`
	MethodFields []string   `json:"methodFields"`
	Methods      [][]string `json:"methods"`
}

func TestWASMRegistrationsMatchProtocolManifest(t *testing.T) {
	raw, err := os.ReadFile("../../protocol/methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest protocolManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || manifest.Error.Envelope == "" || len(manifest.Error.Fields) == 0 ||
		!slices.Equal(manifest.MethodFields, []string{"name", "request", "response", "transfer"}) {
		t.Fatalf("incomplete protocol header: %+v", manifest)
	}
	want := make([]string, 0, len(manifest.Methods))
	seen := map[string]bool{}
	for _, method := range manifest.Methods {
		if len(method) != len(manifest.MethodFields) || slices.Contains(method, "") {
			t.Fatalf("incomplete protocol method: %+v", method)
		}
		name := method[0]
		if seen[name] {
			t.Fatalf("duplicate protocol method %q", name)
		}
		seen[name] = true
		want = append(want, name)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading WASM package: %v", err)
	}
	files := map[string]*ast.File{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing WASM package file %s: %v", name, err)
		}
		if file.Name.Name == "main" {
			files[name] = file
		}
	}
	if len(files) == 0 {
		t.Fatal("parsed WASM package has no main files")
	}
	got := coreRegistrationNames(files)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("WASM registrations do not match protocol\nregistered: %v\nmanifest:   %v", got, want)
	}
}

func coreRegistrationNames(files map[string]*ast.File) []string {
	var names []string
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				index, ok := lhs.(*ast.IndexExpr)
				if !ok {
					continue
				}
				ident, ok := index.X.(*ast.Ident)
				literal, literalOK := index.Index.(*ast.BasicLit)
				if !ok || ident.Name != "core" || !literalOK || literal.Kind != token.STRING {
					continue
				}
				name, err := strconv.Unquote(literal.Value)
				if err == nil {
					names = append(names, name)
				}
			}
			return true
		})
	}
	return names
}

func TestCoreRegistrationNamesIgnoresComments(t *testing.T) {
	source := `package main
func register() {
	core := map[string]any{}
	// core["phantomMethod"] = handler
	core["snapshot"] = handler
}`
	file, err := parser.ParseFile(token.NewFileSet(), "register.go", source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	got := coreRegistrationNames(map[string]*ast.File{"register.go": file})
	if !slices.Equal(got, []string{"snapshot"}) {
		t.Fatalf("registrations = %v, want [snapshot]", got)
	}
}
