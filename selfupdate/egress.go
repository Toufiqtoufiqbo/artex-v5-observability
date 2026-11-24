package selfupdate

import (
	"context"
	"errors"
	"sync/atomic"
)

// VIII.4.4 — self-update network containment.
//
// By default, autonomous self-update network access is DISABLED (fail-closed).
// Enabling is an explicit opt-in (see SetNetworkEnabled). Until enabled, both
// FetchLatest and Stage return an explicit error and do not dial anything.

var networkEnabled atomic.Bool

// IsNetworkEnabled reports whether autonomous self-update network calls are
// allowed. Defaults to false.
func IsNetworkEnabled() bool { return networkEnabled.Load() }

// SetNetworkEnabled enables or disables self-update network access (VIII.4.4).
func SetNetworkEnabled(v bool) { networkEnabled.Store(v) }

func requireNetworkEnabled() error {
	if !networkEnabled.Load() {
		return errors.New("self-update disabled: VIII.4.4 containment (network access not authorized)")
	}
	return nil
}

var _ = context.Background // reserved for future per-call context propagation
