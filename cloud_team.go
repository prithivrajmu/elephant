package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type TeamProposal struct {
	ID       string `json:"id"`
	Author   string `json:"author"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
	Approved bool   `json:"approved"`
	Retired  bool   `json:"retired"`
	Memory   Memory `json:"memory"`
	Reviewer string `json:"reviewer,omitempty"`
}
type TeamProvenance struct {
	Origin   string `json:"origin"`
	Team     string `json:"team"`
	Proposal string `json:"proposal"`
	Author   string `json:"author"`
	Reviewer string `json:"reviewer"`
	Revision int    `json:"revision"`
	Digest   string `json:"digest"`
}
type teamPage struct {
	Version int            `json:"version"`
	Items   []TeamProposal `json:"items"`
	Cursor  int            `json:"cursor"`
	More    bool           `json:"more"`
}

// TeamCloud performs an explicit proposal/review/withdrawal operation. Review
// JSON must contain the exact revision and digest shown by team-pull.
func TeamCloud(o CloudSyncOptions, input json.RawMessage) (json.RawMessage, error) {
	if o.Endpoint == "" || o.Token == "" || o.Remote.User == "" || o.Remote.Tenant == "" {
		return nil, fmt.Errorf("cloud origin, token and expected identity required")
	}
	data, e := cloudHTTP(o, "/v1/team", input)
	return json.RawMessage(data), e
}

// PullTeamCloud stages approved peer lessons locally. It never grants local
// approval; an existing CLI approve is still required for a new review revision.
func (s Store) PullTeamCloud(id Identity, o CloudSyncOptions, team string) ([]TeamProposal, error) {
	if id.Team == "" || team == "" {
		return nil, fmt.Errorf("local and remote team required")
	}
	policy, e := s.LanguagePolicy()
	if e != nil {
		return nil, e
	}
	out := []TeamProposal{}
	cursor := 0
	for pages := 0; pages < 10; pages++ {
		req, _ := json.Marshal(map[string]any{"action": "pull", "team": team, "cursor": cursor})
		data, e := TeamCloud(o, req)
		if e != nil {
			return out, e
		}
		var p teamPage
		if e = json.Unmarshal(data, &p); e != nil || p.Version != 1 || p.Cursor < cursor || len(p.Items) > 128 {
			return out, fmt.Errorf("invalid team page")
		}
		e = s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
			if _, e := tx.Exec("CREATE TABLE IF NOT EXISTS cloud_team_links(id TEXT PRIMARY KEY,revision INTEGER NOT NULL)"); e != nil {
				return nil, e
			}
			for _, v := range p.Items {
				if v.Memory.Tenant != o.Remote.Tenant || v.Author != v.Memory.Owner || v.Revision < 1 {
					return nil, fmt.Errorf("invalid team provenance")
				}
				key := o.Endpoint + "\n" + o.Remote.Tenant + "\n" + team + "\n" + v.ID
				sum := sha256.Sum256([]byte(key))
				localID := "team-" + hex.EncodeToString(sum[:])
				var revision int
				e := tx.QueryRow("SELECT revision FROM cloud_team_links WHERE id=?", localID).Scan(&revision)
				if e != nil && e != sql.ErrNoRows {
					return nil, e
				}
				if revision >= v.Revision {
					continue
				}
				if !v.Approved && !v.Retired && v.Reviewer == "" {
					continue
				}
				m := v.Memory
				m.Sharing = &TeamProvenance{Origin: o.Endpoint, Team: team, Proposal: v.ID, Author: v.Author, Reviewer: v.Reviewer, Revision: v.Revision, Digest: v.Digest}
				m.ID = localID
				m.Tenant = id.Tenant
				m.Owner = id.User
				m.Team = id.Team
				m.Scope = "team"
				m.Approved = false
				m.Retired = v.Retired || !v.Approved
				m.Helpful = 0
				m.Unhelpful = 0
				if e := Validate(m); e != nil {
					return nil, e
				}
				report := CheckLanguage(policy, m.Lesson)
				if !report.Accepted {
					return nil, fmt.Errorf("team lesson fails local writing policy")
				}
				m.Writing = &report
				if e := applyEvent(tx, Event{Kind: "put", Memory: &m}); e != nil {
					return nil, e
				}
				if _, e := tx.Exec("INSERT INTO cloud_team_links VALUES(?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision", localID, v.Revision); e != nil {
					return nil, e
				}
			}
			return nil, nil
		})
		if e != nil {
			return out, e
		}
		out = append(out, p.Items...)
		cursor = p.Cursor
		if !p.More {
			return out, nil
		}
	}
	return out, fmt.Errorf("team page limit reached")
}
