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
	idleGuard    *idleCommitGuard
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
	q.profileMS = float64(profileElapsed) / float64(time.Millisecond)
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
	case "dream_memory":
		var q DreamRequest
		if err := decode(args, &q); err != nil {
			return nil, err
		}
		return s.dream(q)
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
	case "record_memory", "commit_dream":
		var m Memory
		if err := decode(args, &m); err != nil {
			return nil, reject("invalid", err)
		}
		m.Tenant = s.Identity.Tenant
		m.Owner = s.Identity.User
		m.Team = s.Identity.Team
		m.idleGuard = s.idleGuard
		if name == "commit_dream" {
			// A backend must support both halves; never silently commit to a
			// local store when a remote backend is configured.
			if _, ok := s.memoryBackend().(dreamBackend); !ok {
				return nil, fmt.Errorf("configured backend does not support dreaming")
			}
			if m.Dream == nil {
				return nil, fmt.Errorf("dream provenance required")
			}
			if (m.Scope == "project" && (s.Unattached || m.Project != s.Project)) ||
				(m.Scope == "conversation" && (s.Conversation == "" || m.Conversation != s.Conversation)) {
				return nil, fmt.Errorf("dream is outside the configured context")
			}
			m.Supersedes = nil
			for _, c := range m.Dream.Checks {
				m.Supersedes = append(m.Supersedes, c.ID)
			}
		} else {
			if m.Dream != nil {
				return nil, fmt.Errorf("use commit_dream for derived memories")
			}
			m.Project = s.Project
			m.Conversation = s.Conversation
			if s.Unattached {
				m.Project = ""
			}
		}
		if m.Scope == "" {
			m.Scope = "personal"
		}
		if m.Features == nil && name != "commit_dream" {
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
