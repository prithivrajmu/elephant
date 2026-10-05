package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These are synthetic examples, not usable credentials.
var sensitiveExamples = []struct {
	name   string
	kind   string
	secret string
}{
	{"pem", "PEM private key", "-----BEGIN PRIVATE KEY-----"},
	{"pem_rsa", "PEM private key", "-----BEGIN RSA PRIVATE KEY-----"},
	{"pem_ec", "PEM private key", "-----BEGIN EC PRIVATE KEY-----"},
	{"pem_encrypted", "PEM private key", "-----BEGIN ENCRYPTED PRIVATE KEY-----"},
	{"pem_openssh", "PEM private key", "-----BEGIN OPENSSH PRIVATE KEY-----"},
	{"aws_akia", "AWS access key ID", "AKIA0000000000000000"},
	{"aws_asia", "AWS access key ID", "ASIA0000000000000000"},
	{"github_personal", "GitHub token", "ghp_" + strings.Repeat("a", 36)},
	{"github_oauth", "GitHub token", "gho_" + strings.Repeat("a", 36)},
	{"github_user", "GitHub token", "ghu_" + strings.Repeat("a", 36)},
	{"github_server", "GitHub token", "ghs_" + strings.Repeat("a", 36)},
	{"github_refresh", "GitHub token", "ghr_" + strings.Repeat("a", 40)},
	{"github_fine_grained", "GitHub token", "github_pat_" + strings.Repeat("a", 22) + "_" + strings.Repeat("b", 59)},
	{"slack_app", "Slack token", "xoxa-000000000000-aaaaaaaaaaaaaaaaaaaaaaaa"},
	{"slack_bot", "Slack token", "xoxb-000000000000-aaaaaaaaaaaaaaaaaaaaaaaa"},
	{"slack_user", "Slack token", "xoxp-000000000000-aaaaaaaaaaaaaaaaaaaaaaaa"},
	{"slack_refresh", "Slack token", "xoxr-000000000000-aaaaaaaaaaaaaaaaaaaaaaaa"},
	{"slack_session", "Slack token", "xoxs-000000000000-aaaaaaaaaaaaaaaaaaaaaaaa"},
	{"anthropic", "Anthropic API key", "sk-ant-api03-" + strings.Repeat("a", 40)},
	{"openai", "OpenAI-style API key", "sk-" + strings.Repeat("a", 32)},
	{"openai_project", "OpenAI-style API key", "sk-proj-" + strings.Repeat("a_0-", 12)},
	{"google", "Google API key", "AIza" + strings.Repeat("a_0-", 8) + "a_0"},
	{"stripe_secret", "Stripe live key", "sk_live_" + strings.Repeat("a", 24)},
	{"stripe_restricted", "Stripe live key", "rk_live_" + strings.Repeat("a", 24)},
	{"jwt", "JWT", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJzeW50aGV0aWMifQ.c3ludGhldGljX3NpZ25hdHVyZQ"},
	{"url_https", "URL with embedded credentials", "https://synthetic:synthetic-password@example.invalid/path"},
	{"url_database", "URL with embedded credentials", "postgres://synthetic:p%40ss@example.invalid:5432/db"},
	{"github_prefix_payload", "GitHub token", "github_pat_a"},
	{"slack_prefix_payload", "Slack token", "xoxb-a"},
	{"anthropic_prefix_payload", "Anthropic API key", "sk-ant-a"},
	{"stripe_prefix_payload", "Stripe live key", "rk_live_a"},
}

var sensitiveFields = []struct {
	name string
	set  func(*Memory, string)
}{
	{"incident", func(m *Memory, s string) { m.Incident = s }},
	{"lesson", func(m *Memory, s string) { m.Lesson = s }},
	{"source", func(m *Memory, s string) { m.Source = s }},
	{"subject", func(m *Memory, s string) { m.Subject = s }},
	{"conversation", func(m *Memory, s string) { m.Conversation = s }},
	{"features key", func(m *Memory, s string) { m.Features = map[string][]string{s: {"synthetic"}} }},
	{"features value", func(m *Memory, s string) { m.Features = map[string][]string{"synthetic": {s}} }},
	{"requires key", func(m *Memory, s string) { m.Requires = map[string][]string{s: {"synthetic"}} }},
	{"requires value", func(m *Memory, s string) { m.Requires = map[string][]string{"synthetic": {s}} }},
	{"excludes key", func(m *Memory, s string) { m.Excludes = map[string][]string{s: {"synthetic"}} }},
	{"excludes value", func(m *Memory, s string) { m.Excludes = map[string][]string{"synthetic": {s}} }},
}

func checkSensitiveError(t *testing.T, err error, field, kind, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("credential accepted")
	}
	message := err.Error()
	if !strings.Contains(message, "in "+field+" (") || !strings.Contains(message, kind) {
		t.Fatalf("error does not identify field and pattern kind: %s", message)
	}
	if strings.Contains(message, secret) {
		t.Fatal("error echoed credential")
	}
	if !strings.Contains(message, "rephrase or redact") {
		t.Fatalf("error does not suggest remediation: %s", message)
	}
}

func TestScreenSensitivePatterns(t *testing.T) {
	for _, example := range sensitiveExamples {
		t.Run(example.name, func(t *testing.T) {
			for _, field := range sensitiveFields {
				t.Run(field.name, func(t *testing.T) {
					var m Memory
					field.set(&m, "Observed "+example.secret+" in synthetic evidence.")
					checkSensitiveError(t, ScreenSensitive(m), field.name, example.kind, example.secret)
				})
			}
		})
	}
}

func TestScreenSensitiveBenign(t *testing.T) {
	for _, text := range []string{
		"rotate the API token", "password policy", "sk-", "Bearer <token>",
		"43d3f83", "0123456789abcdef0123456789abcdef01234567",
		"f160b363-65a1-4dc8-8748-1a073795c710", strings.Repeat("abcdef01", 8),
		"-----BEGIN PUBLIC KEY-----", "AKIA000000000000000", "AKIA00000000000000000",
		"ghp_" + strings.Repeat("a", 35), "AIza" + strings.Repeat("a", 34),
		"AIza" + strings.Repeat("a", 36), "sk-" + strings.Repeat("a", 31),
		"github_pat_", "xoxa-", "xoxb-", "xoxp-", "xoxr-", "xoxs-", "sk-ant-", "sk-proj-", "sk_live_", "rk_live_",
		"sk_test_" + strings.Repeat("a", 24), "eyJheader.eyJpayload", "eyJheader.eyJpayload.",
		"https://example.invalid/path", "https://synthetic@example.invalid/path",
		"https://synthetic:@example.invalid/path", "https://:synthetic@example.invalid/path",
	} {
		t.Run(text, func(t *testing.T) {
			for _, field := range sensitiveFields {
				var m Memory
				field.set(&m, text)
				if err := ScreenSensitive(m); err != nil {
					t.Fatalf("benign %s rejected: %v", field.name, err)
				}
			}
		})
	}
}

func TestScreenSensitiveRecallContracts(t *testing.T) {
	data, err := os.ReadFile("testdata/recall-contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	// Check every fixture string, including keys and non-memory request text.
	var checkStrings func(any)
	checkStrings = func(value any) {
		switch value := value.(type) {
		case string:
			for _, field := range sensitiveFields {
				var m Memory
				field.set(&m, value)
				if err := ScreenSensitive(m); err != nil {
					t.Fatal(err)
				}
			}
		case []any:
			for _, item := range value {
				checkStrings(item)
			}
		case map[string]any:
			for key, item := range value {
				checkStrings(key)
				checkStrings(item)
			}
		}
	}
	checkStrings(document)
	var contracts struct {
		Memories []Memory `json:"memories"`
	}
	if err := json.Unmarshal(data, &contracts); err != nil {
		t.Fatal(err)
	}
	if len(contracts.Memories) == 0 {
		t.Fatal("no contract memories checked")
	}
	store := Store{Path: filepath.Join(t.TempDir(), "memories.sqlite")}
	for _, m := range contracts.Memories {
		if _, err := store.Record(m); err != nil {
			t.Fatalf("contract memory %s failed to record: %v", m.ID, err)
		}
	}
}

func TestScreenSensitiveStoreRecordUnchanged(t *testing.T) {
	store := Store{Path: filepath.Join(t.TempDir(), "memories.sqlite")}
	_, _, m := fixture()
	if _, err := store.Record(m); err != nil {
		t.Fatal(err)
	}
	before, err := store.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range sensitiveExamples {
		t.Run(example.name, func(t *testing.T) {
			for _, field := range sensitiveFields {
				t.Run(field.name, func(t *testing.T) {
					_, _, candidate := fixture()
					field.set(&candidate, example.secret)
					_, err := store.Record(candidate)
					checkSensitiveError(t, err, field.name, example.kind, example.secret)
				})
			}
		})
	}
	after, err := store.All()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected records changed the store", err)
	}
	db, err := store.database(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var events int
	if err := db.QueryRow("SELECT count(*) FROM events").Scan(&events); err != nil || events != 1 {
		t.Fatal("rejected records changed audit history", events, err)
	}
}

func TestScreenSensitiveStoreRecordNoCreation(t *testing.T) {
	dir := t.TempDir()
	store := Store{Path: filepath.Join(dir, "memories.sqlite")}
	_, _, m := fixture()
	m.Source = "sk-" + strings.Repeat("a", 32)
	_, err := store.Record(m, &TaskCapture{Task: "synthetic task", Session: "synthetic session"})
	checkSensitiveError(t, err, "source", "OpenAI-style API key", m.Source)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejected record created store files", err)
	}
}

func TestScreenSensitiveExistingStoreStillLoads(t *testing.T) {
	_, _, m := fixture()
	m.Source = "sk-" + strings.Repeat("a", 32)
	if err := Validate(m); err != nil {
		t.Fatal("Validate must remain compatible with existing records", err)
	}
	data, err := json.Marshal(Event{Kind: "put", Memory: &m})
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Path: filepath.Join(t.TempDir(), "legacy.jsonl")}
	if err := os.WriteFile(store.Path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	all, err := store.All()
	if err != nil || len(all) != 1 || all[0].Source != m.Source {
		t.Fatal("existing credential-bearing memory failed to load", err)
	}
	all, err = store.All()
	if err != nil || len(all) != 1 || all[0].Source != m.Source {
		t.Fatal("migrated credential-bearing memory failed to reload", err)
	}
}
