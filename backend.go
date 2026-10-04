package memory

// MemoryBackend is the memory lifecycle boundary used by tool transports.
// Implementations must enforce Identity and scope before returning candidates.
// Filesystem hooks, diagnostics, backup and the dashboard continue to use Store.
type MemoryBackend interface {
	Recall(Identity, Request) (Result, error)
	Record(Memory, ...*TaskCapture) (Memory, error)
	Feedback(Identity, string, string, string, bool, ...string) error
	Retire(Identity, string) error
}

var _ MemoryBackend = Store{}

func (s Service) memoryBackend() MemoryBackend {
	if s.Backend != nil {
		return s.Backend
	}
	return s.Store
}
