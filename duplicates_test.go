package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestRecordNearDuplicateChoices(t *testing.T) {
	_, _, m := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "memory.sqlite")}
	m.Scope, m.Project, m.Requires = "project", "app", nil
	// Two lessons an agent saved in separate tasks for the same zsh failure.
	m.Incident = "A Bash command used echo ======= as a separator between git show outputs; zsh treated the leading = as command-path expansion and failed."
	m.Lesson = "In this zsh-based Bash tool, never start an unquoted word with =. Zsh EQUALS expansion looks it up as a command. Use quoted separators."
	first, err := s.Record(m)
	if err != nil {
		t.Fatal(err)
	}
	reworded := m
	reworded.Scope = "personal" // personal lessons are recalled here too
	reworded.Incident = "A Bash command used echo ====== as a section separator between git show outputs and failed in zsh."
	reworded.Lesson = "In zsh, an unquoted word starting with = triggers =command expansion and fails with not found. Quote separators."
	reworded.Source = "another task"
	if _, err = s.Record(reworded); err == nil || !strings.Contains(err.Error(), "["+first.ID+"] scope=project") {
		t.Fatalf("reworded lesson saved as new: %v", err)
	}

	// A project lesson from another project is not recallable here.
	other := reworded
	other.Scope, other.Project = "project", "other-app"
	if _, err = s.Record(other); err != nil {
		t.Fatalf("lesson from an unrelated project blocked: %v", err)
	}
	// Sharing with the team is an explicit act, not a duplicate.
	shared := reworded
	shared.Scope, shared.Team = "team", "platform"
	if _, err = s.Record(shared); err != nil {
		t.Fatalf("team draft blocked: %v", err)
	}

	// supersedes must name a lesson the author can recall here.
	reworded.Supersedes = []string{"not-a-lesson"}
	if _, err = s.Record(reworded); err == nil || !strings.Contains(err.Error(), "supersedes names not-a-lesson") {
		t.Fatalf("unknown supersedes accepted: %v", err)
	}
	// supersedes retires the old lesson in the same save and keeps the link.
	reworded.Supersedes = []string{first.ID}
	updated, err := s.Record(reworded)
	if err != nil || updated.ID == first.ID || len(updated.Supersedes) != 1 || updated.Supersedes[0] != first.ID {
		t.Fatalf("supersede not saved: %+v %v", updated, err)
	}
	retry, err := s.Record(reworded)
	if err != nil || retry.ID != updated.ID {
		t.Fatalf("supersede retry not idempotent: %+v %v", retry, err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if x.ID == first.ID && !x.Retired {
			t.Fatal("superseded lesson still active")
		}
	}

	// distinct_from saves a different lesson and is not stored.
	sibling := m
	sibling.Lesson = "In zsh, an unquoted word that starts with = is a command lookup. Quote the separator, or the && chain stops."
	if _, err = s.Record(sibling); err == nil || !strings.Contains(err.Error(), updated.ID) {
		t.Fatalf("sibling not refused: %v", err)
	}
	sibling.DistinctFrom = []string{updated.ID}
	confirmed, err := s.Record(sibling)
	if err != nil || confirmed.DistinctFrom != nil {
		t.Fatalf("confirmed distinct lesson not saved cleanly: %+v %v", confirmed, err)
	}
	unrelated := m
	unrelated.Incident = "BSD sed failed on macOS."
	unrelated.Lesson = "On macOS, BSD sed -i needs an explicit backup suffix, so pass an empty one."
	if _, err = s.Record(unrelated); err != nil {
		t.Fatalf("unrelated lesson refused: %v", err)
	}
	unrelated.Supersedes = make([]string, 11)
	if _, err = s.Record(unrelated); err == nil {
		t.Fatal("unbounded supersedes accepted")
	}
}

var listedID = regexp.MustCompile(`\n\[([0-9a-f]{32})\] scope=`)

// Replays real lessons in save order through Record. Ratchet: raise caught or
// lower refusedDistinct when the measure improves; never the other way.
// Known misses are the two paraphrases (5da8ef62, 198824e6); the one refusal is
// 075e5242, a related cost lesson that partly contradicts 1ea8ec71.
func TestNearDuplicateReplayRatchet(t *testing.T) {
	const caughtFloor, refusedDistinctCeiling = 1, 1
	var replay struct {
		Lessons []struct {
			ID, Scope, Incident, Lesson string
		}
		Duplicates [][2]string
	}
	data, err := os.ReadFile("testdata/duplicate-lessons.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	duplicate := map[[2]string]bool{}
	for _, d := range replay.Duplicates {
		duplicate[d] = true
	}
	s := Store{Path: filepath.Join(t.TempDir(), "memory.sqlite")}
	_, _, base := fixture()
	base.Project, base.Requires = "elephant", nil
	stored := map[string]string{} // store ID -> fixture ID
	caught, refusedDistinct := 0, 0
	for _, l := range replay.Lessons {
		m := base
		m.Scope, m.Incident, m.Lesson = l.Scope, l.Incident, l.Lesson
		saved, err := s.Record(m)
		if err == nil {
			stored[saved.ID] = l.ID
			continue
		}
		if !strings.HasPrefix(err.Error(), "a similar lesson is already saved") {
			t.Fatalf("%s: %v", l.ID, err)
		}
		hit := false
		for _, id := range listedID.FindAllStringSubmatch(err.Error(), -1) {
			hit = hit || duplicate[[2]string{l.ID, stored[id[1]]}]
		}
		if hit {
			caught++
		} else {
			refusedDistinct++
			t.Logf("%s refused although no listed lesson is a duplicate:\n%v", l.ID, err)
		}
		// The agent checked the listed lessons and resends as a distinct lesson.
		for _, id := range listedID.FindAllStringSubmatch(err.Error(), -1) {
			m.DistinctFrom = append(m.DistinctFrom, id[1])
		}
		if saved, err = s.Record(m); err != nil {
			t.Fatalf("%s: %v", l.ID, err)
		}
		stored[saved.ID] = l.ID
	}
	t.Logf("caught %d of %d duplicates; refused %d distinct lessons", caught, len(replay.Duplicates), refusedDistinct)
	if caught < caughtFloor || refusedDistinct > refusedDistinctCeiling {
		t.Fatalf("near-duplicate ratchet regressed: caught %d (floor %d), refused distinct %d (ceiling %d)", caught, caughtFloor, refusedDistinct, refusedDistinctCeiling)
	}
}
