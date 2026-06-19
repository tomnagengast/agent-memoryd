package version

import "testing"

func TestStringOmitsEmptyReleaseMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version = "v0.1.4"
	Commit = ""
	Date = ""

	if got, want := String(), "memoryd v0.1.4"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringIncludesStampedSourceBuildMetadata(t *testing.T) {
	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version = "abc123"
	Commit = "abc123"
	Date = "2026-06-19T12:00:00Z"

	if got, want := String(), "memoryd abc123 (abc123, 2026-06-19T12:00:00Z)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
