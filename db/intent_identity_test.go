package db

import (
	"strings"
	"testing"
)

// Phase IX.2.2 — intent identity contract (Gate B). Pure unit tests; no DB.

func TestIntentFingerprintContract_v922(t *testing.T) {
	base := IntentIdentityInput{ExplorationID: 7, Summary: "Test /login for SQLi", AssetIDs: []int64{3, 1}}

	cases := []struct {
		name string
		in   IntentIdentityInput
		same bool // same fingerprint as base?
	}{
		{"identical", base, true},
		{"case and whitespace normalised", IntentIdentityInput{ExplorationID: 7, Summary: "  test   /LOGIN for   sqli ", AssetIDs: []int64{3, 1}}, true},
		{"asset order and duplicates ignored", IntentIdentityInput{ExplorationID: 7, Summary: "Test /login for SQLi", AssetIDs: []int64{1, 3, 3, 1}}, true},
		{"different exploration scope", IntentIdentityInput{ExplorationID: 8, Summary: "Test /login for SQLi", AssetIDs: []int64{3, 1}}, false},
		{"different asset set", IntentIdentityInput{ExplorationID: 7, Summary: "Test /login for SQLi", AssetIDs: []int64{3}}, false},
		{"different path", IntentIdentityInput{ExplorationID: 7, Summary: "Test /admin for SQLi", AssetIDs: []int64{3, 1}}, false},
		{"different query parameter", IntentIdentityInput{ExplorationID: 7, Summary: "Test /login?id=1 for SQLi", AssetIDs: []int64{3, 1}}, false},
		{"punctuation is meaningful", IntentIdentityInput{ExplorationID: 7, Summary: "Test /login for SQLi!", AssetIDs: []int64{3, 1}}, false},
	}
	for _, tc := range cases {
		got := IntentFingerprint(tc.in)
		want := IntentFingerprint(base)
		if (got == want) != tc.same {
			t.Errorf("%s: same=%v got=%s want=%s", tc.name, got == want, got, want)
		}
	}
}

func TestIntentFingerprintStructuralMode_v922(t *testing.T) {
	a := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "测试 /login 布尔盲注", Operation: "sqli-test", Resource: "/login?id=1"})
	b := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "completely different wording", Operation: "SQLI-TEST", Resource: "/login?id=1"})
	if a != b {
		t.Fatal("structural identity must ignore summary wording")
	}
	c := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "测试 /login 布尔盲注", Operation: "xss-test", Resource: "/login?id=1"})
	if a == c {
		t.Fatal("different operation must change identity")
	}
	d := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "测试 /login 布尔盲注", Operation: "sqli-test", Resource: "/login?id=2"})
	if a == d {
		t.Fatal("different parameter must change identity")
	}
}

func TestIntentFingerprintFormat_v922(t *testing.T) {
	fp := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "x"})
	if !strings.HasPrefix(fp, IntentIdentityVersion+":") {
		t.Fatalf("fingerprint %q missing version prefix", fp)
	}
	if len(fp) != len(IntentIdentityVersion)+1+64 {
		t.Fatalf("fingerprint %q length = %d, want version + 64 hex chars", fp, len(fp))
	}
	if fp != IntentFingerprint(IntentIdentityInput{ExplorationID: 1, Summary: "x"}) {
		t.Fatal("fingerprint is not deterministic")
	}
}

func TestIntentFingerprintEmptyInput_v922(t *testing.T) {
	if got := IntentFingerprint(IntentIdentityInput{}); !strings.HasPrefix(got, IntentIdentityVersion+":") {
		t.Fatalf("empty input fingerprint = %q", got)
	}
	// Empty text and an all-invalid asset list must still be stable.
	a := IntentFingerprint(IntentIdentityInput{ExplorationID: 1, AssetIDs: []int64{0, -1}})
	b := IntentFingerprint(IntentIdentityInput{ExplorationID: 1})
	if a != b {
		t.Fatal("non-positive asset ids must be ignored")
	}
}
