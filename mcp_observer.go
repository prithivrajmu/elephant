package memory

import "time"

// MCPClientInfo is the client-provided implementation identity from initialize.
// It identifies an application, not an authenticated user or a chat session.
type MCPClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Title   string `json:"title,omitempty"`
}

// MCPEvent reports connection identity or a completed known tool call. It never
// contains tool arguments, results, errors, prompts, or memory text. A nonempty
// Tool identifies a completed call; otherwise Client identifies initialization.
type MCPEvent struct {
	Client MCPClientInfo `json:"client,omitempty"`
	Tool   string        `json:"tool,omitempty"`
	At     time.Time     `json:"at"`
	Failed bool          `json:"failed,omitempty"`
}
