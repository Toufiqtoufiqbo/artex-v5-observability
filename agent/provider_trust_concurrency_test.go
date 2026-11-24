package agent

import (
	"sync"
	"testing"
)

// VIII.4.8 Gate C — deterministic concurrency: trusted-provider policy state
// updated while requests read. Barrier-based (no arbitrary sleeps).

func TestTrustSetConcurrentSeedAndRead(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"seed.example"})

	var wg sync.WaitGroup
	seedDone := make(chan struct{})
	// Writer: late seed attempts must be rejected (first seed wins).
	wg.Add(1)
	go func() {
		defer wg.Done()
		SeedTrustedProviderHosts("late.example")
		SeedTrustedProviderHosts("another.example")
		close(seedDone)
	}()
	// Readers: policy decisions for trusted + untrusted hosts must be stable
	// and never observe a partially-updated set.
	const readers = 16
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, trusted := ProviderPolicyForBaseURL("https://seed.example/v1")
				if !trusted {
					t.Error("seed.example must stay trusted while seedDone is not closed")
					return
				}
				set := TrustedProviderHosts()
				if len(set) != 1 || set[0] != "seed.example" {
					t.Errorf("trust set = %v, want exactly [seed.example]", set)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	if set := TrustedProviderHosts(); len(set) != 1 || set[0] != "seed.example" {
		t.Fatalf("trust set after concurrent seed = %v, want [seed.example]", set)
	}
	_ = seedDone
}

func TestTrustPolicyReadDuringReseed(t *testing.T) {
	resetProviderTrustedHosts(t)
	SetTrustedProviderHosts([]string{"base.example"})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		SeedTrustedProviderHosts("sneaky.example")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 500; i++ {
			if _, ok := ProviderPolicyForBaseURL("https://sneaky.example/v1"); ok {
				t.Error("late-seeded host must never authorize")
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	if _, ok := ProviderPolicyForBaseURL("https://sneaky.example/v1"); ok {
		t.Fatal("late-seeded host must never authorize (first-seed-wins)")
	}
}
