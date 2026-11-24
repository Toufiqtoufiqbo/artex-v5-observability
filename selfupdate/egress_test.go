package selfupdate

import (
	"context"
	"strings"
	"testing"
)

// VIII.4.4 — self-update network containment tests.

func TestSelfUpdateDisabledByDefault(t *testing.T) {
	if IsNetworkEnabled() {
		t.Fatalf("self-update network must be disabled by default (VIII.4.4)")
	}
}

func TestSelfUpdateBlockedWhenDisabled(t *testing.T) {
	// Both entry points must refuse to dial when disabled.
	if _, err := FetchLatest(context.Background(), NewClient("")); err == nil {
		t.Fatalf("FetchLatest must be blocked when disabled")
	} else if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected 'disabled' in error, got: %v", err)
	}
	if err := Stage(context.Background(), NewClient(""), &Release{TagName: "v0.3.15"}, "v0.3.14", nil); err == nil {
		t.Fatalf("Stage must be blocked when disabled")
	}
	SetNetworkEnabled(true)
	defer SetNetworkEnabled(false)
	if !IsNetworkEnabled() {
		t.Fatalf("expected enabled after SetNetworkEnabled(true)")
	}
	SetNetworkEnabled(false)
	if IsNetworkEnabled() {
		t.Fatalf("expected disabled after SetNetworkEnabled(false)")
	}
}
