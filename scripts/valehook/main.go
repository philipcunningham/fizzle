// Command valehook is a Claude Code PostToolUse hook. It reads the hook
// payload from stdin and runs vale on the file touched by a Write or
// Edit call. When vale reports problems the hook emits a "block"
// decision on stdout, which feeds the findings back to the agent. All
// linting policy lives in .vale.ini; files vale has no format for
// produce no alerts, and a machine without vale is never blocked.
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
	if _, err := exec.LookPath("vale"); err != nil {
		return
	}
	// #nosec G204 G702 -- the command is the fixed string "vale" and rel
	// is passed as an argument (never shell-interpreted), validated to
	// sit inside the project root.
	cmd := exec.Command("vale", "--output=line", rel)
	cmd.Dir = root
	out, runErr := cmd.CombinedOutput()
	if runErr == nil || len(out) == 0 {
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(hookOutput{
		Decision: "block",
		Reason:   "vale found issues in " + rel + ":\n" + string(out),
	})
}
