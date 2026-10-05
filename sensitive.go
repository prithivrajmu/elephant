package memory

import (
	"fmt"
	"regexp"
)

var sensitivePatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"PEM private key", regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)},
	{"AWS access key ID", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]+)`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]+`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]+`)},
	{"OpenAI-style API key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{32,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}(?:$|[^0-9A-Za-z_-])`)},
	{"Stripe live key", regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]+`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)},
	{"URL with embedded credentials", regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]*://[^\s/:@?#]+:[^\s/@?#]+@[^\s/@?#]+`)},
}

// ScreenSensitive rejects common credential formats in authored memory content.
// It does not detect PII or every secret, validate the memory, or redact content.
// Errors identify only the field and pattern kind, never the matching text.
func ScreenSensitive(m Memory) error {
	for _, field := range []struct {
		name string
		text string
	}{
		{"incident", m.Incident},
		{"lesson", m.Lesson},
		{"source", m.Source},
		{"subject", m.Subject},
		{"conversation", m.Conversation},
	} {
		if err := screenSensitiveField(field.name, field.text); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name   string
		labels map[string][]string
	}{
		{"features", m.Features},
		{"requires", m.Requires},
		{"excludes", m.Excludes},
	} {
		for key, values := range field.labels {
			// Label keys can themselves contain credentials. Do not include them
			// in error messages, even when reporting a matching value.
			if err := screenSensitiveField(field.name+" key", key); err != nil {
				return err
			}
			for _, value := range values {
				if err := screenSensitiveField(field.name+" value", value); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func screenSensitiveField(field, text string) error {
	for _, pattern := range sensitivePatterns {
		if pattern.re.MatchString(text) {
			return fmt.Errorf("sensitive content in %s (%s); rephrase or redact credentials before recording", field, pattern.kind)
		}
	}
	return nil
}
