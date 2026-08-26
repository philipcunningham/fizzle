package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

const cliReferencePath = "../../docs/cli-reference.md"

func TestCLIReferenceIsCurrent(t *testing.T) {
	want := generateCLIReference(newApp())
	if os.Getenv("UPDATE_CLI_REFERENCE") == "true" {
		if err := os.WriteFile(cliReferencePath, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(cliReferencePath)
	if err != nil {
		t.Fatal(err)
	}
	// Git may materialise the tracked Markdown with CRLF on Windows while
	// the generator intentionally emits canonical LF. Compare content, not
	// the checkout's platform line endings.
	got = canonicalLF(got)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; regenerate with UPDATE_CLI_REFERENCE=true go test ./cmd/fizzle -run TestCLIReferenceIsCurrent", filepath.ToSlash(cliReferencePath))
	}
}

func TestCanonicalLFPreservesContentWhileNormalisingWindowsLines(t *testing.T) {
	got := canonicalLF([]byte("first\r\nsecond\r\n"))
	if string(got) != "first\nsecond\n" {
		t.Fatalf("canonical LF = %q", got)
	}
}

func canonicalLF(source []byte) []byte {
	return bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
}

func TestCLIReferenceCoversCommandTree(t *testing.T) {
	reference := string(generateCLIReference(newApp()))
	walkCommands(newApp(), nil, func(path []string, command *cli.Command) {
		heading := "## `" + strings.Join(path, " ") + "`"
		if !strings.Contains(reference, heading) {
			t.Errorf("reference is missing command heading %s", heading)
		}
		for _, flag := range command.Flags {
			for _, name := range flag.Names() {
				if !strings.Contains(sectionFor(reference, heading), "`--"+name+"`") {
					t.Errorf("reference section %s is missing flag --%s", heading, name)
				}
			}
		}
	})
}

func generateCLIReference(root *cli.Command) []byte {
	var out strings.Builder
	out.WriteString("# fizzle CLI reference\n\n")
	out.WriteString("This file is generated from the command metadata used by the executable. Don't edit it by hand. Regenerate it with:\n\n")
	out.WriteString("```sh\nUPDATE_CLI_REFERENCE=true go test ./cmd/fizzle -run TestCLIReferenceIsCurrent\n```\n\n")
	walkCommands(root, nil, func(path []string, command *cli.Command) {
		fmt.Fprintf(&out, "## `%s`\n\n", strings.Join(path, " "))
		if command.Usage != "" {
			out.WriteString(command.Usage)
			out.WriteString("\n\n")
		}
		synopsis := strings.Join(path, " ")
		if command.ArgsUsage != "" {
			synopsis += " " + command.ArgsUsage
		}
		fmt.Fprintf(&out, "Usage: `%s`\n\n", synopsis)
		if len(command.Flags) > 0 {
			out.WriteString("Flags:\n\n")
			flags := append([]cli.Flag(nil), command.Flags...)
			sort.Slice(flags, func(i, j int) bool { return flags[i].Names()[0] < flags[j].Names()[0] })
			for _, flag := range flags {
				names := flag.Names()
				rendered := make([]string, len(names))
				for i, name := range names {
					rendered[i] = "`--" + name + "`"
				}
				fmt.Fprintf(&out, "- %s: %s\n", strings.Join(rendered, ", "), flagUsage(flag))
			}
			out.WriteString("\n")
		}
		if text := strings.TrimSpace(command.UsageText); text != "" {
			out.WriteString("Details and examples:\n\n```text\n")
			out.WriteString(text)
			out.WriteString("\n```\n\n")
		}
	})
	return []byte(strings.TrimRight(out.String(), "\n") + "\n")
}

func walkCommands(command *cli.Command, parent []string, visit func([]string, *cli.Command)) {
	path := append(append([]string(nil), parent...), command.Name)
	visit(path, command)
	children := append([]*cli.Command(nil), command.Commands...)
	sort.Slice(children, func(i, j int) bool { return children[i].Name < children[j].Name })
	for _, child := range children {
		if !child.Hidden {
			walkCommands(child, path, visit)
		}
	}
}

func flagUsage(flag cli.Flag) string {
	value := reflect.Indirect(reflect.ValueOf(flag))
	if value.IsValid() && value.Kind() == reflect.Struct {
		field := value.FieldByName("Usage")
		if field.IsValid() && field.Kind() == reflect.String && field.String() != "" {
			return field.String()
		}
	}
	return flag.String()
}

func sectionFor(reference, heading string) string {
	start := strings.Index(reference, heading)
	if start < 0 {
		return ""
	}
	section := reference[start+len(heading):]
	if end := strings.Index(section, "\n## `"); end >= 0 {
		section = section[:end]
	}
	return section
}
