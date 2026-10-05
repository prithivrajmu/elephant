package memory

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// Read the committed inputs only: this gate must not need a module cache or network.
func TestThirdPartyNotices(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	notices, err := os.ReadFile("THIRD_PARTY_NOTICES")
	if err != nil {
		t.Fatal(err)
	}
	noticesText := strings.ReplaceAll(string(notices), "\r\n", "\n") // tolerate CRLF checkouts on Windows
	requires := make(map[string]string)
	inRequire := false
	scanner := bufio.NewScanner(strings.NewReader(string(mod)))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "//", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "require" {
			fields = fields[1:]
			if len(fields) == 1 && fields[0] == "(" {
				inRequire = true
				continue
			}
		} else if inRequire {
			if line == ")" {
				inRequire = false
				continue
			}
		} else {
			continue
		}
		if len(fields) != 2 {
			t.Fatalf("unexpected go.mod require line: %q", line)
		}
		requires[fields[0]] = fields[1]
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	// Each module section is the generator's linked-module manifest.
	linked := make(map[string]string)
	for _, line := range strings.Split(noticesText, "\n") {
		if !strings.HasPrefix(line, "=== ") || !strings.HasSuffix(line, " ===") {
			continue
		}
		title := strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ===")
		if title == "Go standard library/runtime" {
			continue
		}
		fields := strings.Fields(title)
		if len(fields) != 2 {
			t.Fatalf("invalid module heading: %q", line)
		}
		module, version := fields[0], fields[1]
		if _, exists := linked[module]; exists {
			t.Errorf("duplicate notice section for %s", module)
		}
		linked[module] = version
		if want, ok := requires[module]; !ok || want != version {
			t.Errorf("notice for %s has version %s; go.mod has %q; regenerate with python3 scripts/generate_notices.py", module, version, want)
		}
	}
	for _, module := range []string{
		"modernc.org/sqlite",
		"modernc.org/libc",
		"golang.org/x/sys",
		"github.com/google/uuid",
		"github.com/dustin/go-humanize",
		"github.com/mattn/go-isatty",
		"github.com/ncruces/go-strftime",
		"github.com/remyoudompheng/bigfft",
		"modernc.org/mathutil",
		"modernc.org/memory",
	} {
		if _, ok := linked[module]; !ok {
			t.Errorf("missing linked module %s in THIRD_PARTY_NOTICES", module)
		}
	}
	if !strings.Contains(noticesText, "=== Go standard library/runtime ===\nSPDX license identifier (guess): BSD-3-Clause\n\n--- LICENSE ---\nCopyright") {
		t.Error("missing Go standard library/runtime LICENSE section")
	}
	for _, text := range []string{"--- LICENSE-SQLITE ---", "SQLite Is Public Domain", "--- LICENSE-3RD-PARTY.md ---", "musl as a whole is licensed"} {
		if !strings.Contains(noticesText, text) {
			t.Errorf("missing embedded third-party notice %q", text)
		}
	}
}
