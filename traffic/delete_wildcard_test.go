package traffic

import "testing"

// A host filter containing SQL LIKE metacharacters (legal in hostnames, e.g.
// underscore) must match literally: the old unescaped pattern let one host's
// delete/page filter sweep unrelated hosts.
func TestHostFilterAndDeleteEscapeLikeWildcards(t *testing.T) {
	tr, _ := openTraffic(t)
	tr.record(newFlow("api_example.com", "GET", "/one", nil, []byte(`{"ok":1}`)))
	tr.record(newFlow("apiXexample.com", "GET", "/two", nil, []byte(`{"ok":2}`)))

	rows, _, err := tr.Page(PageQuery{Host: "api_example.com", RespMin: -1, RespMax: -1}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Host != "api_example.com" {
		t.Fatalf("page host filter matched %#v, want only api_example.com", rows)
	}

	n, err := tr.DeleteHost("api_example.com")
	if err != nil || n != 1 {
		t.Fatalf("DeleteHost=(%d,%v), want (1,nil)", n, err)
	}
	var remaining int
	if err := tr.DB().QueryRow(`SELECT COUNT(*) FROM exchanges WHERE host='apiXexample.com'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("unrelated host was deleted: %d rows remain, want 1", remaining)
	}
}
