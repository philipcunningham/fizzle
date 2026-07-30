// Command guardhook is a Claude Code PreToolUse hook. It denies Write
// and Edit calls that target protected paths: real-hardware corpus
// fixtures and generated artefacts that only build steps may touch.
// The web parameter schema joins the list when slice 3 makes it a
// generated file. A deliberate change to a protected path is made by
// the operator, or by removing the path from this list in the same
// change that justifies it.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Prefixes are project-root relative with forward slashes.
var protectedPrefixes = []string{
	"testdata/",
	"internal/licenses/THIRD_PARTY_LICENSES.txt",
	"internal/licenses/LICENSE.txt",
	"internal/licenses/sbom.cdx.json",
	"fizzle.cdx.json",
	"web/app/package-lock.json",
}

type hookInput struct {
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

type hookOutput struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
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

	for _, prefix := range protectedPrefixes {
		if rel == prefix || strings.HasPrefix(rel, prefix) {
			var out hookOutput
			out.HookSpecificOutput.HookEventName = "PreToolUse"
			out.HookSpecificOutput.PermissionDecision = "deny"
			out.HookSpecificOutput.PermissionDecisionReason = rel +
				" is protected (corpus fixtures and generated artefacts are never edited by hand; see scripts/guardhook)"
			_ = json.NewEncoder(os.Stdout).Encode(out)
			return
		}
	}
}
