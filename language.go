package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Target is a product setting, not a measured fraction of the ASD-STE100 standard.
type LanguagePolicy struct {
	Standard       string `json:"standard"`
	Issue          int    `json:"issue"`
	Target         int    `json:"target_percent"`
	Mode           string `json:"mode"`
	FullValidation bool   `json:"full_standard_validation"`
}
type LanguageIssue struct {
	Rule     string `json:"rule"`
	Sentence int    `json:"sentence,omitempty"`
	Message  string `json:"message"`
}
type LanguageReport struct {
	Policy   LanguagePolicy  `json:"policy"`
	Checks   []string        `json:"checks"`
	Issues   []LanguageIssue `json:"issues"`
	Accepted bool            `json:"accepted"`
	Note     string          `json:"note"`
}

func NewLanguagePolicy(target int) (LanguagePolicy, error) {
	if target < 80 || target > 100 {
		return LanguagePolicy{}, fmt.Errorf("STE target must be 80 to 100")
	}
	mode := "guide"
	if target == 100 {
		mode = "strict-local-checks"
	}
	return LanguagePolicy{Standard: "ASD-STE100", Issue: 9, Target: target, Mode: mode, FullValidation: false}, nil
}
func (s Store) LanguagePolicy() (LanguagePolicy, error) {
	p, _ := NewLanguagePolicy(80)
	name := s.Path + ".settings.json"
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return p, fmt.Errorf("language settings must be a regular file of 4096 bytes or less")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return p, err
	}
	var stored struct {
		Target int `json:"ste_target"`
	}
	if err = json.Unmarshal(data, &stored); err != nil {
		return p, fmt.Errorf("read language settings: %w", err)
	}
	return NewLanguagePolicy(stored.Target)
}
func (s Store) SetLanguagePolicy(target int) (LanguagePolicy, error) {
	p, err := NewLanguagePolicy(target)
	if err != nil {
		return p, err
	}
	err = s.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
		data, _ := json.MarshalIndent(map[string]int{"ste_target": target}, "", "  ")
		f, e := os.CreateTemp(filepath.Dir(s.Path), ".elephant-settings-")
		if e != nil {
			return nil, e
		}
		name := f.Name()
		defer os.Remove(name)
		if _, e = f.Write(append(data, '\n')); e != nil {
			f.Close()
			return nil, e
		}
		if e = f.Sync(); e != nil {
			f.Close()
			return nil, e
		}
		if e = f.Close(); e != nil {
			return nil, e
		}
		return nil, os.Rename(name, s.Path+".settings.json")
	})
	return p, err
}
func LanguageInstructions(p LanguagePolicy) string {
	return fmt.Sprintf("Use ASD-STE100 as the only writing guide for Elephant summaries. The Elephant target is %d%% (%s). This target is not a compliance score. Use short, direct sentences. Give one instruction in each sentence. Use active verbs. Use one term for one concept. Keep each summary sentence at 25 words or less. Keep technical terms, code, commands, identifiers and source evidence exact. Check meaning and grammar against the official standard. The local checks cover sentence length, selected complex phrases and selected contractions only. They do not check the full dictionary, parts of speech or all writing rules. At 100%%, new summaries must pass all local checks. Do not change historical evidence to make it pass.", p.Target, p.Mode)
}
func (s Service) Instructions() (string, error) {
	p, err := s.Store.LanguagePolicy()
	if err != nil {
		return "", err
	}
	return AgentInstructions + "\n\n" + LanguageInstructions(p), nil
}

// CheckLanguage is a small writing aid. It is deliberately not a full STE parser.
func CheckLanguage(p LanguagePolicy, text string) LanguageReport {
	r := LanguageReport{Policy: p, Checks: []string{"summary sentence length (25 words)", "selected complex phrases", "selected contractions"}, Issues: []LanguageIssue{}, Accepted: true, Note: "Local checks only. The target is not measured ASD-STE100 compliance. Source evidence and code are not rewritten."}
	// Treat code spans as one technical item. Preserve them in stored text.
	var protected strings.Builder
	inCode := false
	for _, c := range text {
		if c == '`' {
			inCode = !inCode
			if inCode {
				protected.WriteString(" CODE ")
			}
			continue
		}
		if !inCode {
			protected.WriteRune(c)
		}
	}
	plain := protected.String()
	if inCode {
		r.Issues = append(r.Issues, LanguageIssue{Rule: "code-delimiter", Message: "Close each code span with a backtick."})
	}
	sentences := strings.FieldsFunc(plain, func(c rune) bool { return c == '.' || c == '!' || c == '?' || c == '\n' })
	for i, sentence := range sentences {
		tokens := strings.FieldsFunc(sentence, func(c rune) bool { return unicode.IsSpace(c) })
		if len(tokens) > 25 {
			r.Issues = append(r.Issues, LanguageIssue{Rule: "sentence-length", Sentence: i + 1, Message: fmt.Sprintf("Sentence %d has %d words. Use 25 words or less.", i+1, len(tokens))})
		}
	}
	lower := " " + strings.ToLower(strings.Join(strings.Fields(strings.Map(func(c rune) rune {
		if unicode.IsLetter(c) || unicode.IsNumber(c) || c == '\'' {
			return c
		}
		if c == '’' {
			return '\''
		}
		return ' '
	}, plain)), " ")) + " "
	for _, pair := range [][2]string{{"in order to", "to"}, {"utilize", "use"}, {"utilise", "use"}, {"leverage", "use"}, {"prior to", "before"}, {"subsequent to", "after"}, {"at this point in time", "now"}} {
		if strings.Contains(lower, " "+pair[0]+" ") {
			r.Issues = append(r.Issues, LanguageIssue{Rule: "complex-phrase", Message: "Use " + pair[1] + " instead of " + pair[0] + "."})
		}
	}
	for _, word := range []string{"don't", "can't", "won't", "isn't", "aren't", "doesn't", "shouldn't", "couldn't", "wouldn't"} {
		if strings.Contains(lower, " "+word+" ") {
			r.Issues = append(r.Issues, LanguageIssue{Rule: "contraction", Message: "Write " + word + " in full."})
		}
	}
	r.Accepted = p.Target < 100 || len(r.Issues) == 0
	return r
}
