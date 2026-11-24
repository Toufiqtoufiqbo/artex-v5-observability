package db

import "testing"

// Regression for IX.2.1.1: a producer that omits `name` must still produce a
// distinct identity per endpoint, using the summary as the fallback name.
func TestFindingNameOrDefaultEmptyName_v923(t *testing.T) {
	cases := []struct {
		name, summary, want string
	}{
		{"", "SQLi on /login?id=1", "SQLi on /login?id=1"},
		{"   ", "  SQLi on /api?id=2  ", "SQLi on /api?id=2"},
		{"Explicit title", "summary ignored", "Explicit title"},
	}
	for _, tc := range cases {
		if got := FindingNameOrDefault(tc.name, tc.summary); got != tc.want {
			t.Errorf("FindingNameOrDefault(%q,%q) = %q, want %q", tc.name, tc.summary, got, tc.want)
		}
	}

	fp1 := FindingFingerprint(42, "SQLi", FindingNameOrDefault("", "SQLi on /login?id=1"), nil)
	fp2 := FindingFingerprint(42, "SQLi", FindingNameOrDefault("", "SQLi on /login?id=2"), nil)
	if fp1 == fp2 {
		t.Fatalf("empty-name findings with different parameters merged: %s", fp1)
	}
}
