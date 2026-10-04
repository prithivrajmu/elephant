import { describe, expect, it } from "vitest";
import fixtures from "../../testdata/recall-contracts.json";
import { recall } from "../src/recall";
import { bytes, labelsSchema, type Memory } from "../src/contracts";

describe("shared local/hosted recall contracts", () => {
  for (const c of fixtures.cases) it(c.name, () => {
    const q = { task: c.request.task, project_id: c.request.profile.project,
      conversation_id: c.request.profile.conversation, context_features: labelsSchema.parse(c.request.profile.features),
      byte_budget: c.request.byte_budget, limit: c.request.limit };
    const result = recall((c.memories ?? fixtures.memories) as Memory[], { tenant: c.identity.tenant, user: c.identity.user }, q,
      c.initialize, Date.parse(c.now));
    expect(result.hits.map(h => h.id)).toEqual(c.expected_ids);
    for(const h of result.hits)expect(Number.isFinite(h.score)).toBe(true);
    expect(result.bytes).toBe(bytes(result.context));
    expect(result.bytes).toBeLessThanOrEqual(q.byte_budget);
  });
});
