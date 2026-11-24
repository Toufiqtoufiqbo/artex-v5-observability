// Package evidence preserves finding evidence independently of disposable traffic.
package evidence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/telemetry"
	"github.com/Autumn-27/artex/traffic"
)

type Store struct {
	DB      *db.DB
	Traffic *traffic.Traffic
	Dir     string
}

func New(pg *db.DB, tr *traffic.Traffic, dir string) *Store {
	return &Store{DB: pg, Traffic: tr, Dir: dir}
}

// EvidenceState classifies whether a finding's bound evidence is usable right
// now. It is computed at read time from the content-addressed blobs and is never
// persisted: a stored "valid" flag would go stale the moment a blob is deleted
// or modified behind the database's back.
type EvidenceState string

const (
	EvidenceValid      EvidenceState = "valid"
	EvidenceInvalid    EvidenceState = "invalid"
	EvidenceNoEvidence EvidenceState = "none"
)

// ErrEvidenceInvalid marks an export or retest that must fail because required
// evidence no longer verifies. Callers must not downgrade it to a warning.
var ErrEvidenceInvalid = errors.New("证据已失效")

// InvalidEvidenceError names the finding (and, when known, the binding/side)
// whose evidence failed verification.
type InvalidEvidenceError struct {
	FindingID int64
	BindingID int64
	Side      string
	Reason    string
}

func (e *InvalidEvidenceError) Error() string {
	if e.BindingID == 0 {
		return fmt.Sprintf("漏洞 %d 的证据已失效：%s", e.FindingID, e.Reason)
	}
	return fmt.Sprintf("漏洞 %d 的证据 #%d（%s）已失效：%s", e.FindingID, e.BindingID, e.Side, e.Reason)
}

func (e *InvalidEvidenceError) Unwrap() error { return ErrEvidenceInvalid }

// EvidenceValidity is the read-time verdict for one finding's evidence. Bindings
// counts every bound snapshot; InvalidBindingIDs lists only the ones that failed
// so an operator can see partial damage instead of a single boolean.
type EvidenceValidity struct {
	State             EvidenceState `json:"state"`
	Bindings          int           `json:"bindings"`
	InvalidBindingIDs []int64       `json:"invalid_binding_ids,omitempty"`
	Detail            string        `json:"detail,omitempty"`
}

// checkSnapshotBody verifies one side of a snapshot against the content-
// addressed store, distinguishing deletion from modification so the caller can
// report the actual failure instead of a generic error.
func (s *Store) checkSnapshotBody(v db.TrafficEvidenceSnapshot, side string) error {
	hash, length := v.ReqHash, v.ReqLen
	if side == "response" {
		hash, length = v.RespHash, v.RespLen
	} else if side != "request" {
		return errors.New("side 必须为 request 或 response")
	}
	path, err := hashPath(s.Dir, hash)
	if err != nil {
		return err
	}
	if _, err = os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return errors.New("正文文件缺失")
		}
		return err
	}
	if err = verifyFile(path, hash, length); err != nil {
		return errors.New("正文哈希或长度不匹配")
	}
	return nil
}

// validateBindings evaluates an already-loaded binding set. It performs no
// database access, so callers can run it inside their own evidence transaction.
func (s *Store) validateBindings(findingID, version int64, bindings []db.FindingTrafficBinding) EvidenceValidity {
	out := EvidenceValidity{State: EvidenceValid, Bindings: len(bindings)}
	if len(bindings) == 0 {
		// A finding that once had evidence and now has none lost its proof. A
		// finding that never bound traffic is simply text-only, not invalid.
		if version > 0 {
			out.State = EvidenceInvalid
			out.Detail = "所有已绑定证据已被移除"
		} else {
			out.State = EvidenceNoEvidence
		}
		return out
	}
	var reasons []string
	for _, b := range bindings {
		bad := false
		// The snapshot id is the digest of the normalized metadata, so a row
		// whose metadata was altered behind the application's back no longer
		// matches it. Body hashes alone would miss a URL/status/method swap.
		if b.Snapshot.ID != db.TrafficSnapshotID(b.Snapshot) {
			bad = true
			reasons = append(reasons, fmt.Sprintf("#%d metadata: 快照元数据哈希不匹配", b.ID))
		}
		for _, side := range []string{"request", "response"} {
			if err := s.checkSnapshotBody(b.Snapshot, side); err != nil {
				bad = true
				reasons = append(reasons, fmt.Sprintf("#%d %s: %v", b.ID, side, err))
			}
		}
		if bad {
			out.InvalidBindingIDs = append(out.InvalidBindingIDs, b.ID)
		}
	}
	if len(out.InvalidBindingIDs) > 0 {
		out.State = EvidenceInvalid
		out.Detail = strings.Join(reasons, "; ")
	}
	return out
}

// ValidateFindingEvidence is the retrieval-time trust check. It holds the
// evidence advisory lock while hashing so a concurrent GC cannot unlink a blob
// between the stat and the hash, and so the binding set cannot change mid-check.
// A database error is returned as an error (unknown state), never as "valid".
func (s *Store) ValidateFindingEvidence(ctx context.Context, findingID int64) (out EvidenceValidity, err error) {
	validationStart := telemetry.StampStartedAt() // IX.2.3
	err = s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		list, e := db.FindingTrafficTx(tx, findingID)
		if e != nil {
			return e
		}
		out = s.validateBindings(findingID, list.Version, list.Bindings)
		return nil
	})
	if err == nil {
		ev := telemetry.NewEvent(telemetry.EvidenceValidated, "evidence", "")
		ev.Stage = "validate"
		ev.Outcome = telemetry.OutcomeOK
		if out.State == EvidenceInvalid {
			ev.Outcome = telemetry.OutcomeFailed
			ev.ErrorClass = telemetry.ErrClassEvidenceIntegrity
		}
		ev.DurationMS = msPtr64(uint64(time.Since(validationStart).Milliseconds()))
		telemetry.Emit(ev)
		reg := telemetry.DefaultRegistry()
		reg.Inc("evidence.validations", nil, 1)
		if out.State == EvidenceInvalid {
			reg.Inc("evidence.failures", map[string]string{"reason": "invalid_bindings"}, 1)
		}
	}
	return
}

func msPtr64(ms uint64) *int64 { v := int64(ms); return &v }

func hashPath(dir, hash string) (string, error) {
	if len(hash) != 64 {
		return "", errors.New("invalid evidence hash")
	}
	if _, err := hex.DecodeString(hash); err != nil || strings.ToLower(hash) != hash {
		return "", errors.New("invalid evidence hash")
	}
	return filepath.Join(dir, "blobs", hash[:2], hash+".bin"), nil
}

func verifyFile(path, hash string, length int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != length || hex.EncodeToString(h.Sum(nil)) != hash {
		return fmt.Errorf("证据正文校验失败: %s", hash)
	}
	return nil
}

// A new body becomes visible only after a durable write. Failed SQL commits may
// leave unreferenced files; GC reaps those after a full day's grace period.
func (s *Store) writeBody(r io.Reader, expectedLength int64, expectedHash string) (hash string, err error) {
	stage := filepath.Join(s.Dir, ".staging")
	if err = os.MkdirAll(stage, 0o700); err != nil {
		return
	}
	f, err := os.CreateTemp(stage, "body-")
	if err != nil {
		return "", err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		return "", err
	}
	hash = hex.EncodeToString(h.Sum(nil))
	if n != expectedLength {
		return "", fmt.Errorf("正文不完整: 预期 %d 字节，读取 %d 字节", expectedLength, n)
	}
	if expectedHash != "" && expectedHash != hash {
		return "", errors.New("原始流量正文哈希不匹配")
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	path, err := hashPath(s.Dir, hash)
	if err != nil {
		return "", err
	}
	if _, err = os.Stat(path); err == nil {
		if err = verifyFile(path, hash, n); err != nil {
			return "", err
		}
		// Refresh the grace period for a restored but not-yet-committed body.
		now := time.Now()
		return hash, os.Chtimes(path, now, now)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer d.Close()
	if runtime.GOOS == "windows" {
		// Windows cannot FlushFileBuffers a directory handle ("Access is
		// denied"); the SQL transaction still orders the blob's visibility.
		return hash, nil
	}
	return hash, d.Sync()
}

// StageFindingsExport freezes bindings/report versions and makes private body
// copies before the HTTP response is started. The caller owns and removes dest.
func (s *Store) StageFindingsExport(ctx context.Context, findings []*db.DBFinding, dest string, copyBodies bool) error {
	return s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		var snapshots []db.TrafficEvidenceSnapshot
		for _, f := range findings {
			list, err := db.FindingTrafficTx(tx, f.ID)
			if err != nil {
				return err
			}
			// Verify every bound body for every export format, not only the zip
			// path that copies them. CSV/JSON/md-single must never describe
			// tampered or deleted evidence as if it still supported the finding.
			if v := s.validateBindings(f.ID, list.Version, list.Bindings); v.State == EvidenceInvalid {
				return &InvalidEvidenceError{FindingID: f.ID, Reason: v.Detail}
			}
			f.TrafficBindings = list.Bindings
			f.TrafficCount = len(list.Bindings)
			f.EvidenceVersion = list.Version
			f.ReportEvidenceVersion = list.ReportVersion
			if err = tx.QueryRow(`SELECT report FROM findings WHERE id=$1`, f.ID).Scan(&f.Report); err != nil {
				return err
			}
			for _, b := range list.Bindings {
				snapshots = append(snapshots, b.Snapshot)
			}
		}
		if copyBodies {
			return s.copySnapshots(snapshots, dest)
		}
		return nil
	})
}

func (s *Store) prepare(ctx context.Context, refs []db.TrafficRef) ([]db.PreparedTrafficEvidence, error) {
	refs, err := db.NormalizeTrafficRefs(refs)
	if err != nil {
		return nil, err
	}
	out := make([]db.PreparedTrafficEvidence, 0, len(refs))
	if len(refs) == 0 {
		return out, nil
	}
	ids := make([]string, len(refs))
	byID := map[string]db.TrafficRef{}
	for i, ref := range refs {
		ids[i] = ref.TrafficID
		byID[ref.TrafficID] = ref
	}
	err = s.Traffic.ReadEvidence(ctx, ids, func(e traffic.EvidenceExchange) error {
		rh, err := s.writeBody(e.Request, e.ReqLen, e.ReqHash)
		if err != nil {
			return err
		}
		ph, err := s.writeBody(e.Response, e.RespLen, e.RespHash)
		if err != nil {
			return err
		}
		v := db.TrafficEvidenceSnapshot{SourceTrafficID: e.ID, CapturedAt: e.TS, URL: e.URL, Method: e.Method, Status: e.Status, ContentType: e.ContentType,
			ReqHead: e.ReqHead, RespHead: e.RespHead, ReqHash: rh, RespHash: ph, ReqLen: e.ReqLen, RespLen: e.RespLen}
		// Raw wire bytes: normalize once here so the ID, the stored row and every
		// downstream consumer (archive, API responses) all see the same text.
		v = v.Normalize()
		v.ID = db.TrafficSnapshotID(v)
		out = append(out, db.PreparedTrafficEvidence{Ref: byID[e.ID], Snapshot: v})
		return nil
	})
	return out, err
}

func (s *Store) Record(ctx context.Context, in db.RecordFindingInput, refs []db.TrafficRef) (out *db.RecordedFinding, err error) {
	err = s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		if err := db.LockTaskEvidenceTx(tx, in.TaskID); err != nil {
			return err
		}
		prepared, err := s.prepare(ctx, refs)
		if err != nil {
			return err
		}
		out, err = db.RecordFindingTx(ctx, tx, in, prepared)
		return err
	})
	return
}

func (s *Store) Bind(ctx context.Context, findingID int64, refs []db.TrafficRef) (out *db.FindingTraffic, err error) {
	err = s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		if err := db.LockFindingEvidenceTx(tx, findingID, nil); err != nil {
			return err
		}
		prepared, err := s.prepare(ctx, refs)
		if err != nil {
			return err
		}
		if err = db.AddFindingTrafficTx(tx, findingID, prepared); err != nil {
			return err
		}
		out, err = db.FindingTrafficTx(tx, findingID)
		return err
	})
	return
}

func (s *Store) WithBinding(ctx context.Context, findingID, bindingID int64, fn func(db.FindingTrafficBinding) error) error {
	return s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		list, err := db.FindingTrafficTx(tx, findingID)
		if err != nil {
			return err
		}
		for _, b := range list.Bindings {
			if b.ID == bindingID {
				return fn(b)
			}
		}
		return db.ErrEvidenceNotFound
	})
}

// Binding resolves one binding's metadata under the evidence lock and releases
// the lock before returning. Callers that then stream a body to a client must
// use this instead of WithBinding: verifyFile+io.Copy is O(body size), so a
// large download (or a slow client) holding WithEvidenceTx would block every
// evidence write process-wide. Reading the blob afterwards is safe — blobs are
// content-addressed and GC only reaps unreferenced files after a 24h grace
// period, and an already-open fd survives an unlink regardless.
func (s *Store) Binding(ctx context.Context, findingID, bindingID int64) (db.FindingTrafficBinding, error) {
	var out db.FindingTrafficBinding
	err := s.WithBinding(ctx, findingID, bindingID, func(b db.FindingTrafficBinding) error {
		out = b
		return nil
	})
	return out, err
}

// OpenBody may be called without holding the evidence lock; see Binding.
func (s *Store) OpenBody(snapshot db.TrafficEvidenceSnapshot, side string) (*os.File, int64, error) {
	hash, length := snapshot.ReqHash, snapshot.ReqLen
	if side == "response" {
		hash, length = snapshot.RespHash, snapshot.RespLen
	} else if side != "request" {
		return nil, 0, errors.New("side 必须为 request 或 response")
	}
	path, err := hashPath(s.Dir, hash)
	if err != nil {
		return nil, 0, err
	}
	if err = verifyFile(path, hash, length); err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	return f, length, err
}

// CopySnapshots is used by both report downloads and portable task archives.
// The destination owns real copies, never links into either disposable store.
func (s *Store) CopySnapshots(ctx context.Context, snapshots []db.TrafficEvidenceSnapshot, dest string) error {
	return s.DB.WithEvidenceTx(ctx, func(*sql.Tx) error { return s.copySnapshots(snapshots, dest) })
}

func (s *Store) copySnapshots(snapshots []db.TrafficEvidenceSnapshot, dest string) error {
	for _, v := range snapshots {
		for _, side := range []string{"request", "response"} {
			if err := func() error {
				f, length, err := s.OpenBody(v, side)
				if err != nil {
					return err
				}
				defer f.Close()
				hash := v.ReqHash
				if side == "response" {
					hash = v.RespHash
				}
				target, err := hashPath(dest, hash)
				if err != nil {
					return err
				}
				if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					return err
				}
				out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
				if os.IsExist(err) {
					return verifyFile(target, hash, length)
				}
				if err != nil {
					return err
				}
				_, copyErr := io.Copy(out, f)
				syncErr := out.Sync()
				closeErr := out.Close()
				return errors.Join(copyErr, syncErr, closeErr)
			}(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) InstallSnapshots(ctx context.Context, snapshots []db.TrafficEvidenceSnapshot, source string) error {
	return s.WithInstalledSnapshots(ctx, snapshots, source, func() error { return nil })
}

// WithInstalledSnapshots pins installed bodies until the metadata restore finishes.
// The callback must not acquire another evidence advisory lock.
func (s *Store) WithInstalledSnapshots(ctx context.Context, snapshots []db.TrafficEvidenceSnapshot, source string, restore func() error) error {
	return s.DB.WithEvidenceTx(ctx, func(*sql.Tx) error {
		for _, v := range snapshots {
			if v.ID != db.TrafficSnapshotID(v) {
				return errors.New("归档证据快照元数据哈希不匹配")
			}
			for _, body := range []struct {
				hash   string
				length int64
			}{{v.ReqHash, v.ReqLen}, {v.RespHash, v.RespLen}} {
				if err := func() error {
					path, err := hashPath(source, body.hash)
					if err != nil {
						return err
					}
					f, err := os.Open(path)
					if err != nil {
						return err
					}
					defer f.Close()
					_, err = s.writeBody(f, body.length, body.hash)
					return err
				}(); err != nil {
					return err
				}
			}
		}
		return restore()
	})
}

func (s *Store) Collect(ctx context.Context, now time.Time) error {
	return s.DB.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE traffic_evidence_snapshots s SET unreferenced_at=$1
WHERE unreferenced_at IS NULL AND NOT EXISTS(SELECT 1 FROM finding_traffic_bindings b WHERE b.snapshot_id=s.id)`, now); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM traffic_evidence_snapshots s WHERE unreferenced_at<$1
AND NOT EXISTS(SELECT 1 FROM finding_traffic_bindings b WHERE b.snapshot_id=s.id)`, now.Add(-24*time.Hour)); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT req_hash FROM traffic_evidence_snapshots UNION SELECT resp_hash FROM traffic_evidence_snapshots`)
		if err != nil {
			return err
		}
		refs := map[string]bool{}
		for rows.Next() {
			var hash string
			if err := rows.Scan(&hash); err != nil {
				rows.Close()
				return err
			}
			refs[hash] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, root := range []string{filepath.Join(s.Dir, "blobs"), filepath.Join(s.Dir, ".staging")} {
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if os.IsNotExist(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				if refs[strings.TrimSuffix(entry.Name(), ".bin")] {
					return nil
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if now.Sub(info.ModTime()) < 24*time.Hour {
					return nil
				}
				return os.Remove(path)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) RunGC(ctx context.Context) {
	timer := time.NewTicker(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-timer.C:
			if err := s.Collect(ctx, now); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[evidence] cleanup failed: %v", err)
			}
		}
	}
}
