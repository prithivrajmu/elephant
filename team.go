package memory

import "fmt"

// File exchange is a local review workflow, not authenticated enterprise synchronization.
func ExportTeam(s Service) ([]Memory, error) {
	all, err := s.Store.All()
	if err != nil {
		return nil, err
	}
	out := []Memory{}
	for _, m := range all {
		if visible(m, s.Identity, s.Project) && m.Scope == "team" {
			m.Helpful = 0
			m.Unhelpful = 0
			out = append(out, m)
		}
	}
	return out, nil
}
func ImportTeam(s Service, bundle []Memory) ([]string, error) {
	if s.Identity.Team == "" {
		return nil, fmt.Errorf("configure --team before importing peer lessons")
	}
	if len(bundle) > 1000 {
		return nil, fmt.Errorf("max 1000 lessons per import")
	}
	policy, err := s.Store.LanguagePolicy()
	if err != nil {
		return nil, err
	}
	// Validate the entire bundle before making any writes.
	for _, m := range bundle {
		if err := Validate(m); err != nil {
			return nil, err
		}
		if report := CheckLanguage(policy, m.Lesson); !report.Accepted {
			return nil, fmt.Errorf("import summary fails local STE checks: %s", report.Issues[0].Message)
		}
		if m.Scope != "team" || m.Retired {
			return nil, fmt.Errorf("only active team lessons may be imported")
		}
	}
	ids := []string{}
	for _, m := range bundle {
		m.Tenant = s.Identity.Tenant
		m.Team = s.Identity.Team
		m.Owner = s.Identity.User
		m.Approved = false
		recorded, err := s.Store.Record(m)
		if err != nil {
			return ids, err
		}
		ids = append(ids, recorded.ID)
	}
	return ids, nil
}
