package memory

import (
	"encoding/json"
	"fmt"
)

type Service struct {
	Store        Store
	Identity     Identity
	Root         string
	Project      string
	Conversation string
	Unattached   bool
}

func (s Service) Profile() (Profile, error) {
	if s.Unattached {
		return Profile{Conversation: s.Conversation, Features: map[string][]string{}, Evidence: map[string][]string{}}, nil
	}
	p, err := ProfileProject(s.Root, s.Project)
	p.Conversation = s.Conversation
	return p, err
}
func decode(data json.RawMessage, v any) error {
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	return json.Unmarshal(data, v)
}
func (s Service) Call(name string, args json.RawMessage) (any, error) {
	switch name {
	case "profile_memory":
		return s.Profile()
	case "init_memory", "recall_memory":
		var q Request
		if err := decode(args, &q); err != nil {
			return nil, err
		}
		p, err := s.Profile()
		if err != nil {
			return nil, err
		}
		q.Profile = p
		if e := ValidateLabels(q.ContextFeatures); e != nil {
			return nil, e
		}
		for k, v := range q.ContextFeatures {
			q.Profile.Features[k] = v
		}
		q.Initialize = name == "init_memory"
		return s.Store.Recall(s.Identity, q)
	case "record_memory":
		var m Memory
		if err := decode(args, &m); err != nil {
			return nil, err
		}
		m.Tenant = s.Identity.Tenant
		m.Owner = s.Identity.User
		m.Team = s.Identity.Team
		m.Project = s.Project
		m.Conversation = s.Conversation
		if s.Unattached {
			m.Project = ""
		}
		if m.Scope == "" {
			m.Scope = "personal"
		}
		if m.Features == nil {
			p, err := s.Profile()
			if err != nil {
				return nil, err
			}
			m.Features = p.Features
		}
		return s.Store.Record(m)
	case "feedback_memory":
		var a struct {
			ID         string `json:"id"`
			FeedbackID string `json:"feedback_id"`
			Helpful    *bool  `json:"helpful"`
		}
		if err := decode(args, &a); err != nil {
			return nil, err
		}
		if a.Helpful == nil {
			return nil, fmt.Errorf("helpful boolean required")
		}
		err := s.Store.Feedback(s.Identity, s.Project, a.ID, a.FeedbackID, *a.Helpful, s.Conversation)
		return map[string]bool{"ok": err == nil}, err
	case "forget_memory":
		var a struct {
			ID string `json:"id"`
		}
		if err := decode(args, &a); err != nil {
			return nil, err
		}
		err := s.Store.Retire(s.Identity, a.ID)
		return map[string]bool{"ok": err == nil}, err
	}
	return nil, fmt.Errorf("unknown tool %s", name)
}
