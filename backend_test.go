package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestServiceUsesInjectedMemoryBackend(t *testing.T) {
	local := Store{Path: filepath.Join(t.TempDir(), "local.sqlite")}
	remote := Store{Path: filepath.Join(t.TempDir(), "backend.sqlite")}
	s := Service{Store: local, Backend: remote, Identity: Identity{Tenant: "t", User: "u"}, Root: t.TempDir(), Project: "p"}
	v, err := s.Call("record_memory", json.RawMessage(`{"incident":"Redis cache failed.","lesson":"Use Redis cache limits.","source":"test:redis","scope":"project","outcome":"good"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(Memory)
	if all, err := local.All(); err != nil || len(all) != 0 {
		t.Fatalf("default store changed: %v %v", all, err)
	}
	v, err = s.Call("recall_memory", json.RawMessage(`{"task":"redis"}`))
	if err != nil || len(v.(Result).Hits) != 1 || v.(Result).Hits[0].ID != m.ID {
		t.Fatalf("backend recall: %v %v", v, err)
	}
	feedback, _ := json.Marshal(map[string]any{"id": m.ID, "feedback_id": "run-1", "helpful": true})
	if _, err = s.Call("feedback_memory", feedback); err != nil {
		t.Fatal(err)
	}
	all, err := remote.All()
	if err != nil || all[0].Helpful != 1 {
		t.Fatalf("backend feedback: %v %v", all, err)
	}
	forget, _ := json.Marshal(map[string]string{"id": m.ID})
	if _, err = s.Call("forget_memory", forget); err != nil {
		t.Fatal(err)
	}
	all, err = remote.All()
	if err != nil || !all[0].Retired {
		t.Fatalf("backend retire: %v %v", all, err)
	}
}

// This fixture is also executed by the hosted implementation. It checks hard
// scope, applicability, abstention and budget guarantees across both runtimes.
func TestRecallContracts(t *testing.T) {
	type contractCase struct {
		Name       string    `json:"name"`
		Memories   []Memory  `json:"memories"`
		Identity   Identity  `json:"identity"`
		Request    Request   `json:"request"`
		Initialize bool      `json:"initialize"`
		Now        time.Time `json:"now"`
		Expected   []string  `json:"expected_ids"`
	}
	var fixture struct {
		Memories []Memory       `json:"memories"`
		Cases    []contractCase `json:"cases"`
	}
	b, err := os.ReadFile("testdata/recall-contracts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Memories == nil {
				c.Memories = fixture.Memories
			}
			c.Request.Initialize = c.Initialize
			r := Recall(c.Memories, c.Identity, c.Request, c.Now)
			ids := []string{}
			for _, h := range r.Hits {
				ids = append(ids, h.ID)
			}
			if !reflect.DeepEqual(ids, c.Expected) {
				t.Fatalf("got %v, expected %v", ids, c.Expected)
			}
			if r.Bytes != len(r.Context) || r.Bytes > c.Request.ByteBudget {
				t.Fatalf("budget violation: %v", r)
			}
		})
	}
}
