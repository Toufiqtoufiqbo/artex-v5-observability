package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// VIII.4.2 — task-scoped authorization isolation. These tests exercise the real
// path: Worker.execute → SetTaskScopeForTask/ClearTaskScopeForTask, tools →
// doHTTP → scopeGuard → CheckScopeForKey (per task), with the dial layer as the
// boundary oracle.

// newLoopbackServerOn binds a test server to a distinct loopback host so that
// host-based scopes can distinguish it from other servers.
func newLoopbackServerOn(t *testing.T, addr, body string) *httptest.Server {
	t.Helper()
	l, err := net.Listen("tcp", addr+":0")
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func TestScopeIsolation_TwoTasks(t *testing.T) {
	srvA := newLoopbackServerOn(t, "127.0.0.2", "A")
	srvB := newLoopbackServerOn(t, "127.0.0.3", "B")
	defer ClearTaskScopeForTask(101)
	defer ClearTaskScopeForTask(202)

	SetTaskScopeForTask(101, []db.TaskScope{{Kind: "ip", Net: hostOf(srvA.URL) + "/32"}})
	SetTaskScopeForTask(202, []db.TaskScope{{Kind: "ip", Net: hostOf(srvB.URL) + "/32"}})

	ctxA := WithRunInfo(context.Background(), RunInfo{TaskID: 101})
	ctxB := WithRunInfo(context.Background(), RunInfo{TaskID: 202})

	// Task A: own destination allowed.
	res, err := doHTTP(ctxA, httpCall{Method: "GET", URL: srvA.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 || strings.TrimSpace(string(res.Body)) != "A" {
		t.Fatalf("task A own destination: err=%v res=%+v body=%q", err, res, string(res.Body))
	}
	// Task A: other task's destination denied.
	if _, err := doHTTP(ctxA, httpCall{Method: "GET", URL: srvB.URL, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("task A cross-task: got %v, want ErrScopeDenied", err)
	}
	// Task B: own destination allowed.
	res, err = doHTTP(ctxB, httpCall{Method: "GET", URL: srvB.URL, NoJar: true, Timeout: 5 * time.Second})
	if err != nil || res.Status != 200 || strings.TrimSpace(string(res.Body)) != "B" {
		t.Fatalf("task B own destination: err=%v res=%+v body=%q", err, res, string(res.Body))
	}
	// Task B: other task's destination denied.
	if _, err := doHTTP(ctxB, httpCall{Method: "GET", URL: srvA.URL, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("task B cross-task: got %v, want ErrScopeDenied", err)
	}
}

func TestScopeIsolation_ConcurrentOverlap(t *testing.T) {
	srvA := newLoopbackServerOn(t, "127.0.0.2", "A")
	srvB := newLoopbackServerOn(t, "127.0.0.3", "B")
	defer ClearTaskScopeForTask(301)
	defer ClearTaskScopeForTask(302)

	SetTaskScopeForTask(301, []db.TaskScope{{Kind: "ip", Net: hostOf(srvA.URL) + "/32"}})
	SetTaskScopeForTask(302, []db.TaskScope{{Kind: "ip", Net: hostOf(srvB.URL) + "/32"}})

	type step struct {
		tid   int64
		url   string // own destination: must succeed
		other string // other destination: must be denied
		want  string
	}
	steps := []step{{301, srvA.URL, srvB.URL, "A"}, {302, srvB.URL, srvA.URL, "B"}}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan string, len(steps))
	for _, s := range steps {
		wg.Add(1)
		go func(s step) {
			defer wg.Done()
			<-start // force both workers into overlap
			ctx := WithRunInfo(context.Background(), RunInfo{TaskID: s.tid})
			res, err := doHTTP(ctx, httpCall{Method: "GET", URL: s.url, NoJar: true, Timeout: 5 * time.Second})
			if err != nil || res.Status != 200 || strings.TrimSpace(string(res.Body)) != s.want {
				errs <- "task " + strconv.Itoa(int(s.tid)) + " own destination failed: err=" + fmtErr(err)
				return
			}
			if _, err := doHTTP(ctx, httpCall{Method: "GET", URL: s.other, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeDenied) {
				errs <- "task " + strconv.Itoa(int(s.tid)) + " could reach another task's destination: " + fmtErr(err)
			}
		}(s)
	}
	close(start)
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func TestScopeIsolation_NoStaleAfterClear(t *testing.T) {
	srvA := newLoopbackServerOn(t, "127.0.0.2", "A")
	defer ClearTaskScopeForTask(401)

	SetTaskScopeForTask(401, []db.TaskScope{{Kind: "ip", Net: hostOf(srvA.URL) + "/32"}})
	ctxA := WithRunInfo(context.Background(), RunInfo{TaskID: 401})
	if _, err := doHTTP(ctxA, httpCall{Method: "GET", URL: srvA.URL, NoJar: true, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("authorized request failed: %v", err)
	}
	ClearTaskScopeForTask(401)
	if _, err := doHTTP(ctxA, httpCall{Method: "GET", URL: srvA.URL, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("after ClearTaskScopeForTask: got %v, want ErrScopeMissing (no stale inheritance)", err)
	}
}

func TestScopeIsolation_UnseededTaskDenies(t *testing.T) {
	dials := withDialCounter(t)
	srv := newLoopbackServerOn(t, "127.0.0.2", "A")

	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 501})
	defer ClearTaskScopeForTask(501)
	if _, err := doHTTP(ctx, httpCall{Method: "GET", URL: srv.URL, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("unseeded task: got %v, want ErrScopeMissing", err)
	}
	if got := atomic.LoadInt32(dials); got != 0 {
		t.Fatalf("unseeded task dialed %d time(s); must be 0", got)
	}
}

func TestScopeIsolation_CrossTaskDeniedZeroDials(t *testing.T) {
	dials := withDialCounter(t)
	srvA := newLoopbackServerOn(t, "127.0.0.2", "A")
	srvB := newLoopbackServerOn(t, "127.0.0.3", "B")

	SetTaskScopeForTask(601, []db.TaskScope{{Kind: "ip", Net: hostOf(srvA.URL) + "/32"}})
	defer ClearTaskScopeForTask(601)
	defer ClearTaskScopeForTask(602)
	// Task 602: no scope at all.

	before := atomic.LoadInt32(dials)
	ctxB := WithRunInfo(context.Background(), RunInfo{TaskID: 602})
	_, err := doHTTP(ctxB, httpCall{Method: "GET", URL: srvB.URL, NoJar: true, Timeout: 5 * time.Second})
	if !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("task without scope: got %v, want ErrScopeMissing", err)
	}
	if got := atomic.LoadInt32(dials) - before; got != 0 {
		t.Fatalf("cross-task attempt dialed %d time(s); must be 0", got)
	}
}

func TestScopeIsolation_LegacyGlobalDoesNotAuthorizeTasks(t *testing.T) {
	srvA := newLoopbackServerOn(t, "127.0.0.2", "A")
	defer ClearTaskScopeForTask(701)
	defer ClearTaskScope()

	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: hostOf(srvA.URL) + "/32"}}) // legacy key ""

	ctx := WithRunInfo(context.Background(), RunInfo{TaskID: 701})
	if _, err := doHTTP(ctx, httpCall{Method: "GET", URL: srvA.URL, NoJar: true, Timeout: 5 * time.Second}); !errors.Is(err, ErrScopeMissing) {
		t.Fatalf("legacy global scope must not authorize a task context: got %v, want ErrScopeMissing", err)
	}
}
