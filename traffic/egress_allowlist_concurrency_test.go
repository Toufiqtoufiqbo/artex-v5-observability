package traffic

import (
	"sync"
	"testing"
)

// VIII.4.8 Gate C — deterministic concurrency: traffic allowlist updated while
// requests read. Barrier-based (no arbitrary sleeps).

func TestTrafficAllowlistConcurrentReadWrite(t *testing.T) {
	tr := &Traffic{}
	tr.SetEgressAllowlist([]string{"fixture.example"}, true)

	var wg sync.WaitGroup
	start := make(chan struct{})
	// Writer: replaces the allowlist atomically; readers must see old or new.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			tr.SetEgressAllowlist([]string{"fixture.example", "fixture.example:8443"}, true)
			tr.SetEgressAllowlist([]string{"fixture.example"}, true)
		}
	}()
	const readers = 8
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 400; j++ {
				r := reqFor(t, "http://fixture.example/x")
				if !tr.EgressAllowed(r) {
					t.Errorf("fixture.example must always be allowed under either snapshot")
					return
				}
				hosts := tr.AllowedEgressHosts()
				found := false
				for _, h := range hosts {
					if h == "fixture.example" {
						found = true
					}
					if h == "evil.example" {
						t.Error("evil.example must never appear after atomic update")
						return
					}
				}
				if !found {
					t.Errorf("hosts=%v, want fixture.example present", hosts)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestTrafficAllowlistDeniedHostNeverAppears(t *testing.T) {
	tr := &Traffic{}
	tr.SetEgressAllowlist([]string{"fixture.example"}, true)
	// A denied host must be denied before and after concurrent updates.
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 100; i++ {
			tr.SetEgressAllowlist(nil, true)
			tr.SetEgressAllowlist([]string{"other.example"}, true)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 200; i++ {
			if tr.EgressAllowed(reqFor(t, "http://evil.example/x")) {
				t.Error("evil.example must never be allowed")
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	if tr.EgressAllowed(reqFor(t, "http://evil.example/x")) {
		t.Fatal("evil.example must never be allowed after concurrent updates")
	}
}
