// Package feature holds studio's feature specs: end-to-end tests that drive the
// compiled TUI through a real PTY and a virtual-terminal emulator, like a user
// at a terminal. The specs are behind the `feature` build tag and run via
// `make feature-test` (UNIX only, via creack/pty). See docs/testing-strategy.md.
//
// This file is intentionally untagged so that `go test ./pkg/feature/` without
// the tag reports "no test files" rather than a "build constraints exclude all
// Go files" error.
package feature
