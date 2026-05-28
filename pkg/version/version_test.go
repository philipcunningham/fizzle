package version

import "testing"

// Not parallel: mutates package-level Version, Commit, and Date.
func TestString(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })

	Version = "1.0.0"
	Commit = "abc1234"
	Date = "2024-01-01"
	got := String()
	want := "1.0.0 (abc1234, 2024-01-01)"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// Not parallel: mutates package-level Version, Commit, and Date.
func TestStringDev(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = oldVersion, oldCommit, oldDate })

	Version = "dev"
	Commit = "none"
	Date = "unknown"
	got := String()
	want := "dev"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
