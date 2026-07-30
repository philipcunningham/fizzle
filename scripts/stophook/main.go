// Command stophook is a Claude Code Stop hook: a turn never ends on a
// red tree. It runs the fast checks from the Web UI plan's foundation,
// each leg scoped to what actually changed in the working tree:
//
//   - Go files changed: go build ./... plus the js/wasm build of the
//     browser surface (web/wasm).
//   - web/app files changed: tsc --noEmit plus the Vitest suite.
//
// Slow suites (race tests, integration, golangci-lint) stay in
// make check and CI. A clean working tree runs nothing.
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

type hookOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func changed(root string) (goSide, webSide bool) {
	// #nosec G204 -- fixed git invocation.
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		// Without git state, err on the side of checking everything.
		return true, true
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.HasSuffix(path, ".go") || path == "go.mod" || path == "go.sum" {
			goSide = true
		}
		if strings.HasPrefix(path, "web/app/") && !strings.HasPrefix(path, "web/app/node_modules/") {
			webSide = true
		}
	}
	return goSide, webSide
}

func run(dir string, env []string, name string, args ...string) (bool, string) {
	if _, err := exec.LookPath(name); err != nil {
		return true, ""
	}
	// #nosec G204 G702 -- fixed tool names and arguments.
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

func main() {
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return
		}
		root = wd
	}

	goSide, webSide := changed(root)
	var failures []string

	if goSide {
		if ok, out := run(root, nil, "go", "build", "./..."); !ok {
			failures = append(failures, "go build ./... failed:\n"+out)
		}
		if ok, out := run(root, []string{"GOOS=js", "GOARCH=wasm"}, "go", "build", "./web/wasm/"); !ok {
			failures = append(failures, "js/wasm build of web/wasm failed:\n"+out)
		}
	}

	if webSide {
		app := root + "/web/app"
		// #nosec G703 -- root is the trusted project directory from the
		// hook environment, not user input.
		if _, err := os.Stat(app + "/node_modules"); err == nil {
			if ok, out := run(app, nil, "npx", "tsc", "--noEmit"); !ok {
				failures = append(failures, "tsc --noEmit failed in web/app:\n"+out)
			}
			if ok, out := run(app, nil, "npx", "vitest", "run", "--silent"); !ok {
				failures = append(failures, "vitest failed in web/app:\n"+out)
			}
		}
	}

	if len(failures) == 0 {
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(hookOutput{
		Decision: "block",
		Reason:   "the tree is red; fix before ending the turn.\n\n" + strings.Join(failures, "\n\n"),
	})
}
