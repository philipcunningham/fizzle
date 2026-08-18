// Package wasm pins the browser build surface. The js/wasm tagged file
// imports every core package the Web UI's WASM module needs, so
// `make wasm-check` fails fast when a change breaks the browser target.
package wasm
