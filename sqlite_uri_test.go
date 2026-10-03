package memory

import (
	"net/url"
	"testing"
)

func TestSQLiteFileURI(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\Users\me\memory store.sqlite`, "/C:/Users/me/memory store.sqlite"},
		{`D:\memory#?%&\é.sqlite`, "/D:/memory#?%&/é.sqlite"},
		{"/tmp/memory#?%&/é.sqlite", "/tmp/memory#?%&/é.sqlite"},
	} {
		u, err := url.Parse(sqliteFileURI(tc.path, url.Values{"mode": {"ro"}}))
		if err != nil || u.Host != "" || u.Path != tc.want || u.Query().Get("mode") != "ro" {
			t.Fatalf("wrong SQLite URI for %q: %v %v", tc.path, u, err)
		}
	}
}
