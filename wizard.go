package memory

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// SetupWizard prepares reviewable client files. It never replaces a client's config.
func SetupWizard(s Service, binary, output string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	ask := func(label, value string) (string, error) {
		fmt.Fprintf(out, "%s [%s]: ", label, value)
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("setup stopped: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return value, nil
		}
		return line, nil
	}
	fmt.Fprintln(out, "🐘 Elephant\nPersistent experience for coding agents.\nAgents forget. Elephants don’t.\n\nThis setup creates client files. You must merge them into your client settings.")
	root, err := ask("Project folder (use - for no project)", s.Root)
	if err != nil {
		return err
	}
	if root == "-" {
		s.Unattached = true
		s.Project = ""
		s.Conversation, err = ask("Conversation ID (optional)", s.Conversation)
	} else {
		s.Unattached = false
		s.Root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		s.Project, err = ask("Stable project ID", s.Project)
	}
	if err != nil {
		return err
	}
	if _, err = s.Profile(); err != nil {
		return err
	}
	policy, err := s.Store.LanguagePolicy()
	if err != nil {
		return err
	}
	target, err := ask("STE target (80 to 100; not a compliance score)", strconv.Itoa(policy.Target))
	if err != nil {
		return err
	}
	percent, err := strconv.Atoi(target)
	if err != nil {
		return fmt.Errorf("STE target must be a number")
	}
	if _, err = NewLanguagePolicy(percent); err != nil {
		return err
	}
	if output == "" {
		output = "elephant-setup"
	}
	output, err = ask("Setup output folder", output)
	if err != nil {
		return err
	}
	if _, err = s.Store.SetLanguagePolicy(percent); err != nil {
		return err
	}
	config, err := SetupConfig(s, binary)
	if err != nil {
		return err
	}
	if err = WriteSetup(output, config); err != nil {
		return err
	}
	absolute, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nSetup files: %s\n1. Merge mcp.json or codex-mcp.toml into your client settings.\n2. Add AGENT_INSTRUCTIONS.md to your project guidance.\n3. Restart the client. Confirm that Elephant tools are present.\n4. Use Recall before a real task. Imprint a Memory after an observed result.\n\nOpen Memory Palace with: elephant palace\n", absolute)
	return nil
}
