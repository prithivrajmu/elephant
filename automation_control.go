package memory

import (
	"encoding/json"
	"fmt"
)

// SetAutomaticHooks changes only this Service's installed automatic-hook state.
// Checking the exact context under the installer lock prevents a native action
// from modifying a configuration another client replaced after its preview.
// Explicit CLI/MCP calls remain available while automatic hooks are paused.
func (s Service) SetAutomaticHooks(enabled bool) (AutomationConfig, error) {
	unlock, err := lockIdleConfiguration(s.Root)
	if err != nil {
		return AutomationConfig{}, err
	}
	defer unlock()
	path := automationPath(s.Root)
	config, err := ReadAutomation(path)
	if err != nil {
		return config, err
	}
	if !automationMatchesService(config, s) {
		return config, fmt.Errorf("automatic hooks use another memory context; select the matching project, store and identity")
	}
	config.Enabled = enabled
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return config, err
	}
	if err := safeParents(s.Root, path); err != nil {
		return config, err
	}
	return config, atomicLocalFile(path, append(data, '\n'), 0600)
}

func automationMatchesService(config AutomationConfig, s Service) bool {
	return config.Root == s.Root && config.Store == s.Store.Path && config.Identity == s.Identity && config.Project == s.Project && config.Conversation == s.Conversation && config.Unattached == s.Unattached
}
