package memory

import (
	"testing"
	"time"
)

// Relevance ratchet for the journey "agent starts a coding task; Elephant injects lessons".
// The measure is deterministic counts, not time: lessons injected that the task does not need
// (irrelevant) and needed lessons that were not injected (missing). missing must stay 0 so a
// stricter ranking cannot pass by returning nothing. irrelevantCeiling may be lowered, never raised.
// Reproduce: go test -run TestRecallRelevanceRatchet -v
//
// Baseline before the generic-term gate: 43 irrelevant, 0 missing (every task got the full 6 lessons).
// After: 8 irrelevant, 0 missing. The rest matched on ordinary words such as "after" or "install"; a relative
// score cutoff did not separate them (irrelevant hits scored 0.6-0.86 of the best hit).
// TF-IDF cosine with an absolute threshold: 4 irrelevant, 0 missing. The needed lessons that share the
// fewest words with their task (generated-model, tz-tests) sit just above the threshold.
const irrelevantCeiling = 4

type relevanceCase struct {
	name, task string
	want       []string // IDs of lessons the task needs; empty means recall should stay silent
}

func relevanceCorpus() []Memory {
	lesson := func(id, incident, text string) Memory {
		return Memory{ID: id, Tenant: "eval", Owner: "eval", Project: "evalproj", Scope: "project", Outcome: "good", Incident: incident, Lesson: text,
			Source: "fixture", Features: map[string][]string{"language": {"python"}}, Created: time.Now().UTC(), Updated: time.Now().UTC()}
	}
	return []Memory{
		lesson("api-url", "client.py calls to the /v1 API returned HTTP 410 after the platform migration.", "The production API base URL is https://api.example.test/v3. v1 and v2 return 410 Gone. Set BASE in client.py to the v3 URL; do not add version negotiation."),
		lesson("generated-model", "A hand edit to models_gen.py passed unit tests but failed python3 gen.py --check in CI.", "models_gen.py is generated from schema.json by gen.py. Edit schema.json, then run python3 gen.py. Hand edits are overwritten and fail python3 gen.py --check."),
		lesson("tz-tests", "test_report.py failed under a non-UTC TZ for every change to report.py.", "The hhmm assertion in test_report.py assumes UTC. Run the tests as TZ=UTC python3 test_report.py. A failure under another TZ is not caused by your change; do not edit hhmm or the test."),
		lesson("terraform", "terraform apply hung on a stale state lock after a cancelled run.", "Run terraform force-unlock with the lock ID from the error, only after confirming no other apply is running."),
		lesson("docker", "A Docker build re-installed all npm packages on every change.", "Copy package.json and the lockfile before the source so the install layer stays cached."),
		lesson("eslint", "ESLint scanned dist/ and reported thousands of errors.", "Add dist/ to .eslintignore instead of disabling rules."),
		lesson("go-leak", "go test hung after a worker goroutine leaked.", "Run go test -race -timeout 30s and cancel the context in cleanup so the worker goroutine exits."),
		lesson("npm-lock", "npm install changed package-lock.json and broke the CI install.", "Use npm ci in CI and commit the lockfile from the same Node version."),
		lesson("migration", "migrate.py applied the same Alembic revision twice.", "Run alembic current before alembic upgrade head and never edit an applied revision."),
		lesson("pytest-scope", "A pytest fixture leaked database state between tests.", "Give the fixture function scope or roll back in a yield fixture; do not share a session connection."),
		lesson("requests-timeout", "A requests call hung a worker for ten minutes.", "Always pass timeout= to requests.get and requests.post; the library has no default."),
		lesson("flaky-sleep", "A test failed on slow CI because it used time.sleep.", "Wait on the condition with a retry loop and a deadline instead of a fixed sleep."),
		lesson("git-rebase", "A rebase dropped a commit that had been cherry-picked.", "Run git cherry before rebasing and check git log afterwards."),
		lesson("black-config", "black reformatted vendored files.", "Add the vendor directory to extend-exclude in pyproject.toml."),
	}
}

var relevanceCases = []relevanceCase{
	{"api-url", "Our client still points at an old API. Update client.py to the current production API base URL, then run the tests.", []string{"api-url"}},
	{"generated-model", "Add a required string field `email` to the User model, then run the tests.", []string{"generated-model"}},
	{"tz-tests", "percent() in report.py crashes when total is 0; it should return 0. Fix it and make sure the tests pass.", []string{"tz-tests"}},
	{"go-leak", "The worker test hangs: a goroutine leaks and go test never returns. Fix it.", []string{"go-leak"}},
	{"npm-lock", "npm install fails in CI after the lockfile changed. Fix the install step.", []string{"npm-lock"}},
	{"control-paginate", "page() in pager.py skips the first page. Fix it and run the tests.", nil},
	{"control-dedupe", "unique() in uniq.py loses the original order. Fix it and run the tests.", nil},
	{"control-generic", "Rename the helper function in utils.py and run the tests to make sure they pass.", nil},
}

func TestRecallRelevanceRatchet(t *testing.T) {
	corpus := relevanceCorpus()
	id := Identity{Tenant: "eval", User: "eval"}
	profile := Profile{Project: "evalproj", Features: map[string][]string{"language": {"python"}}}
	irrelevant, missing := 0, 0
	for _, c := range relevanceCases {
		r := Recall(corpus, id, Request{Profile: profile, Task: c.task}, time.Now())
		want := map[string]bool{}
		for _, w := range c.want {
			want[w] = true
		}
		got := map[string]bool{}
		extra := []string{}
		for _, h := range r.Hits {
			got[h.ID] = true
			if !want[h.ID] {
				irrelevant++
				extra = append(extra, h.ID)
			}
		}
		for w := range want {
			if !got[w] {
				missing++
				t.Errorf("%s: needed lesson %s was not injected", c.name, w)
			}
		}
		t.Logf("%-16s injected=%d irrelevant=%v", c.name, len(r.Hits), extra)
	}
	t.Logf("TOTAL irrelevant=%d missing=%d (ceiling %d)", irrelevant, missing, irrelevantCeiling)
	if irrelevant > irrelevantCeiling {
		t.Errorf("irrelevant lessons injected rose to %d; ceiling is %d. Lower the ceiling when you improve it, never raise it.", irrelevant, irrelevantCeiling)
	}
}
