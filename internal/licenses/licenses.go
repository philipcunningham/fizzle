//go:build !release

// Package licenses exposes the project's own license and the bundled
// third-party attribution text via Project and ThirdParty. Two
// compilation paths share the same public surface:
//
//   - Default (no build tag): stubs explaining how to regenerate the
//     embedded text. This lets go build, go test, go vet, and the
//     entire test suite run without depending on `make licenses` first.
//   - With `-tags release`: the embedded text from
//     internal/licenses/LICENSE.txt and
//     internal/licenses/THIRD_PARTY_LICENSES.txt. `make build` adds the
//     tag automatically after running `make licenses`, so release
//     binaries always carry real attribution.
package licenses

const stubMessage = "license data not embedded in this build. Run `make licenses` and rebuild with `-tags release` (or `make build`) to populate.\n"

// Project is the fizzle source-code license. In stub builds it points
// callers at the regeneration command.
var Project = stubMessage

// ThirdParty is the concatenated attribution for every Go module
// the fizzle binary links against. In stub builds it points callers
// at the regeneration command.
var ThirdParty = stubMessage
