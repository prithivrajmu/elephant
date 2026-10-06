package memory

import (
	"encoding/json"
	"fmt"
	"time"
)

type Service struct {
	Store        Store
	Backend      MemoryBackend
	Identity     Identity
	Root         string
	Project      string
	Conversation string
	Unattached   bool
	Task         *TaskCapture
}

func (s Service) Profile() (Profile, error) {
	if s.Unattached {
		return Profile{Conversation: s.Conversation, Features: map[string][]string{}, Evidence: map[string][]string{}}, nil
	}
	p, err := ProfileProject(s.Root, s.Project)
	p.Conversation = s.Conversation
	return p, err
}

func (s Service) RecallWithTrace(q Request, profileElapsed time.Duration) (Result, RecallTrace, error) {
	q.profileMS = float64(profileElapsed.Microseconds()) / 1000
	q.manifestFiles = profileManifestCount(q.Profile)
	return s.Store.RecallWithTrace(s.Identity, q)
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
		profileStart := time.Now()
		p, err := s.Profile()
		if err != nil {
			return nil, err
		}
		q.Profile = p
		q.profileMS = elapsedMS(profileStart)
		q.manifestFiles = profileManifestCount(p)
		if e := ValidateLabels(q.ContextFeatures); e != nil {
			return nil, e
		}
		for k, v := range q.ContextFeatures {
			q.Profile.Features[k] = v
		}
		q.Initialize = name == "init_memory"
		return s.memoryBackend().Recall(s.Identity, q)
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
		return s.memoryBackend().Record(m, s.Task)
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
		err := s.memoryBackend().Feedback(s.Identity, s.Project, a.ID, a.FeedbackID, *a.Helpful, s.Conversation)
		return map[string]bool{"ok": err == nil}, err
	case "forget_memory":
		var a struct {
			ID string `json:"id"`
		}
		if err := decode(args, &a); err != nil {
			return nil, err
		}
		err := s.memoryBackend().Retire(s.Identity, a.ID)
		return map[string]bool{"ok": err == nil}, err
	}
	return nil, fmt.Errorf("unknown tool %s", name)
}
