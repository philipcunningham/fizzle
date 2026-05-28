// Package version holds the fizzle release version. The Version, Commit, and
// Date variables are overridden at build time via -ldflags.
package version

// Build information, set via ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string shown in --version output.
func String() string {
	if Commit == "none" {
		return Version
	}
	return Version + " (" + Commit + ", " + Date + ")"
}
