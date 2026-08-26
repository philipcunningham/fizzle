package main

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
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

	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`core\["([A-Za-z0-9]+)"\]\s*=`).FindAllSubmatch(source, -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, string(match[1]))
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("WASM registrations do not match protocol\nregistered: %v\nmanifest:   %v", got, want)
	}
}
