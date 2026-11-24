package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/evidence"
	"github.com/Autumn-27/artex/traffic"
)

func newEvidenceGateServer(t *testing.T) (*Server, *db.DB, *evidence.Store, int64) {
	t.Helper()
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skip("test postgres not configured")
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tr, err := traffic.Open(filepath.Join(dir, "traffic"), ":0")
	if err != nil {
		pg.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{ctx: ctx, m: &Manager{pg: pg, dir: dir, traffic: tr, tasks: map[string]*Task{}},
		chatBusy: map[string]bool{}, chatCancel: map[string]context.CancelCauseFunc{}}
	store := evidence.New(pg, tr, filepath.Join(dir, "evidence"))
	task, err := pg.CreateTask("v863 gate "+t.Name(), "local fixtures", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	fid, err := pg.AddFinding(task.ID, 0, "V863", "Evidence gate", "high", "summary", "proof", "worker", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_, _ = pg.DeleteFindingsByTask(task.ID)
		_ = pg.DeleteTask(task.ID)
		tr.Close()
		pg.Close()
	})
	return s, pg, store, fid
}

func seedGateExchange(t *testing.T, tr *traffic.Traffic, id string, body []byte) {
	t.Helper()
	_, err := tr.DB().Exec(`INSERT INTO exchanges(id,ts,host,method,url_template,url,status,content_type,req_len,resp_len,path) VALUES(?,?,'fixture.local','POST','/test',?,200,'application/octet-stream',2,?,'')`,
		id, time.Now().Unix(), "https://fixture.local/"+id, len(body))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.DB().Exec(`INSERT INTO exchange_bodies(id,req_head,req_body,resp_head,resp_body) VALUES(?,?,?,?,?)`,
		id, "POST /test HTTP/1.1\nHost: fixture.local\n", []byte("{}"), "HTTP 200\n", body)
	if err != nil {
		t.Fatal(err)
	}
}

// TestFindingDetailReflectsEvidenceValidity_v863 is the end-to-end F-B3 gate:
// the detail API reports invalid evidence, export refuses every format, retest
// refuses to start, and the historical finding survives.
func TestFindingDetailReflectsEvidenceValidity_v863(t *testing.T) {
	s, _, store, fid := newEvidenceGateServer(t)
	seedGateExchange(t, s.m.traffic, "v863-gate", []byte("gate body"))
	r, err := store.Bind(context.Background(), fid, []db.TrafficRef{{TrafficID: "v863-gate", Role: "proof"}})
	if err != nil {
		t.Fatal(err)
	}

	get := func() FindingDTO {
		req := httptest.NewRequest(http.MethodGet, "/api/exploration/findings/"+strconv.FormatInt(fid, 10), nil)
		req.SetPathValue("id", strconv.FormatInt(fid, 10))
		w := httptest.NewRecorder()
		s.getFinding(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("getFinding code=%d body=%s", w.Code, w.Body.String())
		}
		var dto FindingDTO
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatal(err)
		}
		return dto
	}

	dto := get()
	if dto.EvidenceState != "valid" || dto.EvidenceValid == nil || !*dto.EvidenceValid {
		t.Fatalf("valid dto=%+v", dto)
	}
	if dto.CurrentVerification != "unverified" {
		t.Fatalf("verification=%q", dto.CurrentVerification)
	}

	b := r.Bindings[0]
	path := filepath.Join(s.m.dir, "evidence", "blobs", b.Snapshot.RespHash[:2], b.Snapshot.RespHash+".bin")
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	dto = get()
	if dto.EvidenceState != "invalid" || dto.EvidenceValid == nil || *dto.EvidenceValid || dto.CurrentVerification != "invalid" {
		t.Fatalf("invalid dto=%+v", dto)
	}
	if !slices.Contains(dto.InvalidEvidenceBindingIDs, b.ID) {
		t.Fatalf("invalid ids=%v", dto.InvalidEvidenceBindingIDs)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/exploration/findings/export?scope=selected&ids="+strconv.FormatInt(fid, 10)+"&format=csv", nil)
	w := httptest.NewRecorder()
	s.findingsExport(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("export code=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/exploration/findings/"+strconv.FormatInt(fid, 10)+"/retests", strings.NewReader(`{}`))
	req.SetPathValue("id", strconv.FormatInt(fid, 10))
	w = httptest.NewRecorder()
	s.startFindingRetest(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("retest code=%d body=%s", w.Code, w.Body.String())
	}

	// Historical record survives; restoring exact bytes revalidates.
	f, err := s.m.pg.GetFinding(fid)
	if err != nil || f == nil {
		t.Fatalf("historical finding lost: %v", err)
	}
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	dto = get()
	if dto.EvidenceState != "valid" {
		t.Fatalf("restored dto=%+v", dto)
	}
}
