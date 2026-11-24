# PHASE8_4_4 — SELF-UPDATE (Phase D)

## 1. Implementation

`selfupdate/egress.go` (new):

```go
var networkEnabled atomic.Bool                       // default: false (VIII.4.4)

func IsNetworkEnabled() bool                         // false by default
func SetNetworkEnabled(v bool)
func requireNetworkEnabled() error
```

Gate added to `FetchLatest` (`selfupdate/github.go:88`) and `Stage` (`selfupdate/stage.go:47`):

```go
if err := requireNetworkEnabled(); err != nil { return nil, err }  // or return err for Stage
```

Behavior:
- Default: disabled → `FetchLatest`/`Stage` return explicit error and do not dial.
- Enabled (explicit `SetNetworkEnabled(true)`): unchanged behavior.
- Phase's "Do not add automatic code execution" — not added.

## 2. Local tests (`selfupdate/egress_test.go`)

- `TestSelfUpdateDisabledByDefault` — `IsNetworkEnabled()` false at process start.
- `TestSelfUpdateBlockedWhenDisabled` — `FetchLatest` and `Stage` return errors containing "disabled"; enable/disable round-trip works.

```
go test ./selfupdate/ -count=1 → ok
```

## 3. Status

**DISABLED by default** (fail closed). Operational consequence: live self-update network access is unavailable until explicitly enabled; follow-up to wire it to a reviewed config surface.
