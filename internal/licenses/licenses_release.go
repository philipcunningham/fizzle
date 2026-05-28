//go:build release

package licenses

import _ "embed"

//go:embed LICENSE.txt
var Project string

//go:embed THIRD_PARTY_LICENSES.txt
var ThirdParty string

//go:embed sbom.cdx.json
var SBOM string
