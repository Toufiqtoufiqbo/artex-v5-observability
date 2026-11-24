package db

import (
	"strings"
	"testing"
)

func TestFindingFingerprintContract_v921(t *testing.T) {
	const task int64 = 42
	base := FindingFingerprint(task, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 3})

	// wantSame groups must all produce the base fingerprint; wantDifferent
	// groups must each produce a fingerprint different from all others and from
	// the base. Ground truth is the documented v1 identity contract, not ARTEX
	// output, so this table is independent of the implementation under test.
	wantSame := []struct {
		name      string
		task      int64
		vulnClass string
		title     string
		assets    []int64
	}{
		{"identical repeat", task, "SQLi", "Blind SQL Injection (boolean)", []int64{3, 7}},
		{"case + whitespace", task, " sqli ", "  blind   SQL injection (BOOLEAN) ", []int64{7, 3}},
		{"asset order + duplicates", task, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 7, 3, 3}},
		{"different evidence is not identity", task, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 3}},
		{"severity is mutable metadata", task, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 3}},
	}
	for _, tc := range wantSame {
		t.Run("same/"+tc.name, func(t *testing.T) {
			got := FindingFingerprint(tc.task, tc.vulnClass, tc.title, tc.assets)
			if got != base {
				t.Fatalf("identity changed:\n got %s\nwant %s", got, base)
			}
		})
	}

	wantDifferent := []struct {
		name      string
		task      int64
		vulnClass string
		title     string
		assets    []int64
	}{
		{"different vuln class", task, "XSS", "Blind SQL Injection (boolean)", []int64{7, 3}},
		{"different target asset", task, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 4}},
		{"different endpoint asset set", task, "SQLi", "Blind SQL Injection (boolean)", []int64{7}},
		{"different affected parameter", task, "SQLi", "Blind SQL Injection (sort)", []int64{7, 3}},
		{"different HTTP method in title", task, "SQLi", "POST Blind SQL Injection (boolean)", []int64{7, 3}},
		{"different task scope", 43, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 3}},
		{"legacy scope", 0, "SQLi", "Blind SQL Injection (boolean)", []int64{7, 3}},
		{"empty optional name", task, "SQLi", "", []int64{7, 3}},
		{"no assets", task, "SQLi", "Blind SQL Injection (boolean)", nil},
		{"percent-encoding is meaningful", task, "SQLi", "Blind SQL Injection (%62oolean)", []int64{7, 3}},
	}
	seen := map[string]bool{base: true}
	for _, tc := range wantDifferent {
		t.Run("different/"+tc.name, func(t *testing.T) {
			got := FindingFingerprint(tc.task, tc.vulnClass, tc.title, tc.assets)
			if seen[got] {
				t.Fatalf("distinct finding merged into existing identity %s", got)
			}
			seen[got] = true
		})
	}

	// Empty fields are legal: both findings collapse to the same normalized
	// identity, but that identity must still be stable and versioned.
	empty := FindingFingerprint(0, "", "", nil)
	if !strings.HasPrefix(empty, FindingIdentityVersion+":") {
		t.Fatalf("missing version prefix: %s", empty)
	}
	if empty != FindingFingerprint(0, "  ", "  ", []int64{0, -1}) {
		t.Fatal("empty/missing values must canonicalize identically")
	}
}

func TestFindingFingerprintDeterministicAndFingerprintFormat_v921(t *testing.T) {
	a := FindingFingerprint(9, "XSS", "Reflected XSS", []int64{1, 2})
	for i := 0; i < 50; i++ {
		if b := FindingFingerprint(9, "XSS", "Reflected XSS", []int64{2, 1, 2}); b != a {
			t.Fatalf("nondeterministic fingerprint: %s != %s", b, a)
		}
	}
	hexPart := strings.TrimPrefix(a, FindingIdentityVersion+":")
	if len(hexPart) != 64 {
		t.Fatalf("fingerprint payload must be 64 hex chars, got %d", len(hexPart))
	}
	for _, r := range hexPart {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("non-hex character in fingerprint: %q", r)
		}
	}
}

func TestNormalizeIdentityAssets_v921(t *testing.T) {
	cases := []struct {
		in   []int64
		want string
	}{
		{nil, ""},
		{[]int64{}, ""},
		{[]int64{0, -3}, ""},
		{[]int64{10, 2, 10, 2}, "2,10"},
		{[]int64{2, 10}, "2,10"},
	}
	for _, tc := range cases {
		if got := normalizeIdentityAssets(tc.in); got != tc.want {
			t.Errorf("normalizeIdentityAssets(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
