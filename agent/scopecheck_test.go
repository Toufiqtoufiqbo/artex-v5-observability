package agent

import (
	"errors"
	"net"
	"testing"

	"github.com/Autumn-27/artex/db"
)

// seedLoopbackScope seeds an explicit loopback IP scope for tests that exercise
// real network paths through doHTTP. VIII.4.1 — target scope is required.
func seedLoopbackScope(t *testing.T, rawURL string) {
	t.Helper()
	host := hostOf(rawURL)
	if host == "" {
		t.Fatalf("seedLoopbackScope: no host in %q", rawURL)
	}
	if net.ParseIP(host) == nil {
		t.Fatalf("seedLoopbackScope: destination %q is not a loopback IP literal", host)
	}
	ClearTaskScope()
	t.Cleanup(ClearTaskScope)
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: host + "/32"}})
}

func TestCheckScope_RootDomain(t *testing.T) {
	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "root_domain", Domain: "example.com"}})
	defer ClearTaskScope()

	ok := []string{
		"http://example.com/x",
		"http://www.example.com/x",
		"https://EXAMPLE.com:8443/x",
		"http://sub.deep.example.com/",
	}
	for _, u := range ok {
		if err := CheckScope(u); err != nil {
			t.Errorf("CheckScope(%q) = %v, want nil", u, err)
		}
	}
	bad := []string{
		"http://evil.com/",
		"http://example.com.evil.net/",
		"http://notexample.com/",
	}
	for _, u := range bad {
		if err := CheckScope(u); err == nil {
			t.Errorf("CheckScope(%q) = nil, want violation", u)
		}
	}
}

func TestCheckScope_CompanyFailsClosed(t *testing.T) {
	ClearTaskScope()
	cid := int64(1)
	SetTaskScope([]db.TaskScope{{Kind: "company", CompanyID: &cid}})
	defer ClearTaskScope()
	if err := CheckScope("http://anything.example/"); err == nil {
		t.Errorf("company-only scope must not authorize an arbitrary host")
	}
}

func TestCheckScope_IP(t *testing.T) {
	ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Value: "10.0.0.5"}})
	defer ClearTaskScope()

	if err := CheckScope("http://10.0.0.5/admin"); err != nil {
		t.Errorf("CheckScope(10.0.0.5) = %v, want nil", err)
	}
	if err := CheckScope("http://10.0.0.6/"); err == nil {
		t.Errorf("CheckScope(10.0.0.6) = nil, want violation")
	}
}

func TestCheckScope_NoScopeDenies(t *testing.T) {
	ClearTaskScope()
	defer ClearTaskScope()
	// VIII.4.1: unseeded scope is NOT authorization.
	if err := CheckScope("http://anything.example/"); !errors.Is(err, ErrScopeMissing) {
		t.Errorf("unseeded scope: got %v, want ErrScopeMissing", err)
	}
}

func TestCheckScope_EmptyScopeDenies(t *testing.T) {
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope(nil) // explicit empty
	if err := CheckScope("http://anything.example/"); !errors.Is(err, ErrScopeEmpty) {
		t.Errorf("empty scope: got %v, want ErrScopeEmpty", err)
	}
}

func TestCheckScope_MalformedEntriesDeny(t *testing.T) {
	cases := []struct {
		name    string
		entries []db.TaskScope
		want    error
	}{
		{"company without id", []db.TaskScope{{Kind: "company"}}, ErrScopeMalformed},
		{"root_domain without domain", []db.TaskScope{{Kind: "root_domain"}}, ErrScopeMalformed},
		{"subdomain without domain", []db.TaskScope{{Kind: "subdomain"}}, ErrScopeMalformed},
		{"invalid ip value", []db.TaskScope{{Kind: "ip", Value: "not-an-ip"}}, ErrScopeMalformed},
		{"invalid ip net", []db.TaskScope{{Kind: "ip", Net: "not-a-cidr"}}, ErrScopeMalformed},
		{"empty cidr", []db.TaskScope{{Kind: "cidr"}}, ErrScopeMalformed},
		{"invalid cidr", []db.TaskScope{{Kind: "cidr", Net: "10.0.0.0/33"}}, ErrScopeMalformed},
		{"icp without value", []db.TaskScope{{Kind: "icp"}}, ErrScopeMalformed},
		{"unsupported kind", []db.TaskScope{{Kind: "banana", Value: "x"}}, ErrScopeMalformed},
		{"mixed: valid ip then invalid ip", []db.TaskScope{{Kind: "ip", Value: "10.0.0.5"}, {Kind: "ip", Value: "garbage"}}, ErrScopeMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ClearTaskScope()
			defer ClearTaskScope()
			SetTaskScope(tc.entries)
			if err := CheckScope("http://10.0.0.5/"); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestCheckScope_IPNetFormAccepted(t *testing.T) {
	// Worker auto-scope writes ip entries with Net set; that form must authorize.
	ClearTaskScope()
	defer ClearTaskScope()
	SetTaskScope([]db.TaskScope{{Kind: "ip", Net: "127.0.0.1/32"}})
	if err := CheckScope("http://127.0.0.1/x"); err != nil {
		t.Errorf("ip scope via Net form should authorize 127.0.0.1, got %v", err)
	}
	if err := CheckScope("http://127.0.0.2/x"); err == nil {
		t.Errorf("127.0.0.2 must not be authorized by ip scope 127.0.0.1/32")
	}
}
