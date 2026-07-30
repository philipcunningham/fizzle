// Command codehook is a Claude Code PostToolUse hook. It formats and
// lints the code file just touched by a Write or Edit call, and blocks
// with the findings when the file doesn't pass. It runs the same tools
// as make check and CI, scoped to one file, so the three can never
// disagree. Files it has no tooling for produce no output, and a
// machine without a tool installed is never blocked on it.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type hookInput struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

type hookOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func block(reason string) {
	_ = json.NewEncoder(os.Stdout).Encode(hookOutput{Decision: "block", Reason: reason})
}

// run executes a tool and reports whether it passed, with its output.
func run(dir string, env []string, name string, args ...string) (bool, string) {
	if _, err := exec.LookPath(name); err != nil {
		return true, ""
	}
	// #nosec G204 G702 -- name is a fixed tool string chosen below and
	// the path argument stays inside the project root.
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

func main() {
	var in hookInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return
	}
	file := in.ToolInput.FilePath
	if file == "" {
		return
	}
	if _, err := os.Stat(file); err != nil {
		return
	}

	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		root = wd
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		return
	}
	rel = filepath.ToSlash(rel)

	switch {
	case strings.HasSuffix(rel, ".go"):
		// Format in place, then vet the file's package. A package whose
		// files are all js/wasm tagged (the browser modules) is vetted
		// under that target instead.
		if ok, out := run(root, nil, "gofmt", "-w", rel); !ok {
			block("gofmt failed on " + rel + ":\n" + out)
			return
		}
		pkg := "./" + filepath.ToSlash(filepath.Dir(rel))
		ok, out := run(root, nil, "go", "vet", pkg)
		if !ok && strings.Contains(out, "build constraints exclude all Go files") {
			ok, out = run(root, []string{"GOOS=js", "GOARCH=wasm"}, "go", "vet", pkg)
		}
		if !ok {
			block("go vet found issues in " + pkg + ":\n" + out)
			return
		}

	case strings.HasPrefix(rel, "web/app/") && hasAnySuffix(rel, ".ts", ".tsx", ".css", ".mjs", ".json"):
		app := filepath.Join(root, "web", "app")
		sub, err := filepath.Rel(app, file)
		if err != nil {
			return
		}
		sub = filepath.ToSlash(sub)
		if strings.HasPrefix(sub, "dist/") || strings.HasPrefix(sub, "node_modules/") {
			return
		}
		if ok, out := run(app, nil, "npx", "prettier", "--write", "--log-level", "warn", sub); !ok {
			block("prettier failed on " + rel + ":\n" + out)
			return
		}
		if hasAnySuffix(sub, ".ts", ".tsx") {
			if ok, out := run(app, nil, "npx", "eslint", sub); !ok {
				block("eslint found issues in " + rel + ":\n" + out)
				return
			}
		}
	}
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}
