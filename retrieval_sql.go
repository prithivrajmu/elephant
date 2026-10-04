package memory

import "strings"

// Select identity/context candidates before decoding memory payloads. UNION
// allows SQLite to use the tenant/owner and tenant/project indexes separately.
// The pure recall engine still enforces every visibility and applicability rule
// and computes BM25 over the complete eligible corpus (no arbitrary top-K).
func readRecallMemories(q sqlReader, id Identity, p Profile) ([]Memory, error) {
	branches := []string{"SELECT id FROM memories WHERE tenant=? AND owner=? AND scope='personal'"}
	args := []any{id.Tenant, id.User}
	if p.Project != "" {
		branches = append(branches, "SELECT id FROM memories WHERE tenant=? AND project=? AND owner=? AND scope='project'")
		args = append(args, id.Tenant, p.Project, id.User)
	}
	if p.Conversation != "" {
		branches = append(branches, "SELECT id FROM memories WHERE tenant=? AND owner=? AND scope='conversation' AND json_extract(data,'$.conversation')=?")
		args = append(args, id.Tenant, id.User, p.Conversation)
	}
	if id.Team != "" {
		branches = append(branches, "SELECT id FROM memories WHERE tenant=? AND scope='team' AND json_extract(data,'$.team')=? AND json_extract(data,'$.approved')=1")
		args = append(args, id.Tenant, id.Team)
	}
	return readMemoryQuery(q, "SELECT data FROM memories WHERE id IN ("+strings.Join(branches, " UNION ")+") AND json_extract(data,'$.retired')=0 ORDER BY id", args...)
}
