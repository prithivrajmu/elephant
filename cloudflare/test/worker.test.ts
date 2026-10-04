import { env } from "cloudflare:workers";
import { createExecutionContext, runInDurableObject } from "cloudflare:test";
import { beforeAll, describe, expect, it } from "vitest";
import { exportJWK, generateKeyPair, SignJWT } from "jose";
import worker, { type Env } from "../src/index";
import type { Memory } from "../src/contracts";

let signingKey: CryptoKey, publicJWKS: string;
beforeAll(async () => {
  const keys = await generateKeyPair("RS256", { extractable: true });
  signingKey = keys.privateKey;
  publicJWKS = JSON.stringify({ keys: [{ ...await exportJWK(keys.publicKey), kid: "test", alg: "RS256", use: "sig" }] });
});
const config = () => ({ ...(env as unknown as Env), PUBLIC_ORIGIN: "https://elephant.example",
  AUTH_ISSUER: "https://issuer.example", AUTH_AUDIENCE: "elephant", AUTH_JWKS: publicJWKS,
  AUTH_SUBJECTS: '["alice","bob"]', TENANT_ID: crypto.randomUUID() });
async function token(subject = "alice", scope = "memory:read memory:write", issuer = "https://issuer.example", audience = "elephant", expiry = "1h") {
  return new SignJWT({ scope }).setProtectedHeader({ alg: "RS256", kid: "test" }).setIssuer(issuer)
    .setAudience(audience).setSubject(subject).setIssuedAt().setExpirationTime(expiry).sign(signingKey);
}
async function request(cfg: Env, path: string, body: unknown, bearer?: string, extra: Record<string, string> = {}) {
  return worker.fetch(new Request(cfg.PUBLIC_ORIGIN + path, { method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json, text/event-stream",
      Host: new URL(cfg.PUBLIC_ORIGIN).host,
      ...(bearer ? { Authorization: `Bearer ${bearer}` } : {}), ...extra }, body: JSON.stringify(body) }), cfg, createExecutionContext());
}
const lesson = { operation_id: "write-1", scope: "project", project_id: "p", incident: "Redis cache failed.",
  lesson: "Use Redis cache limits.", source: "test:redis", requires: { database: ["redis"] } };
const op = (tool: string, args: unknown) => ({ version: 1, tool, arguments: args });
async function execute(cfg: Env, tool: string, args: unknown, bearer: string) {
  const response = await request(cfg, "/v1/operations", op(tool, args), bearer);
  return { response, data: await response.json() as { result: Memory & { hits: { id: string }[]; candidates_truncated?: boolean }; replayed?: boolean; error?: string } };
}
const stub = (cfg: Env, user = "alice") => cfg.MEMORY.get(cfg.MEMORY.idFromName(JSON.stringify([cfg.TENANT_ID, user])));
async function rpc(cfg: Env, method: string, params: unknown, bearer: string, id = 1) {
  const response = await request(cfg, "/mcp", { jsonrpc: "2.0", id, method, params }, bearer, { "MCP-Protocol-Version": "2025-06-18" });
  const raw = await response.text();
  const dataLine = raw.split("\n").find(l => l.startsWith("data:"));
  const text = dataLine ? dataLine.slice(5).trim() : raw;
  return { status: response.status, data: JSON.parse(text) };
}
describe("authenticated hosted lifecycle", () => {
  it("synchronizes revisions and tombstones, including changes made through MCP",async()=>{
    const cfg=config(),t=await token();
    const call=async(body:unknown,bearer=t)=>{const r=await request(cfg,"/v1/sync",body,bearer);return {status:r.status,data:await r.json() as any}};
    const {operation_id:_op,...payload}=lesson;
    const memory={...payload,id:"local-id",retired:false,created:"2026-01-01T00:00:00Z",updated:"2026-01-02T00:00:00Z"};
    const push={action:"push",operation_id:"sync-1",expected_revision:0,memory};
    const first=await call(push);expect(first.status).toBe(200);expect(first.data.revision).toBe(1);
    expect(first.data.memory.updated).toBe(memory.updated);
    expect((await call(push)).data.replayed).toBe(true);
    expect((await call({...push,operation_id:"different"})).status).toBe(409);
    expect((await call({...push,operation_id:"move-context",expected_revision:1,memory:{...memory,project_id:"other"}})).status).toBe(409);
    expect((await call({...push,operation_id:"invalid-scope",memory:{...memory,project_id:undefined}})).status).toBe(400);
    const p=await call({action:"pull",cursor:0,scopes:["project"]});expect(p.data.items).toHaveLength(1);
    expect((await call({action:"pull",cursor:0,scopes:["project"]},await token("bob"))).data.items).toEqual([]);
    expect((await call(push,await token("alice","memory:read"))).status).toBe(403);
    expect((await request(cfg,"/v1/sync",push,t,{"X-Elephant-User":"bob"})).status).toBe(403);
    await execute(cfg,"forget_memory",{operation_id:"mcp-retire",id:memory.id},t);
    const retired=await call({action:"pull",cursor:p.data.cursor,scopes:["project"]});
    expect(retired.data.items[0]).toMatchObject({revision:2,memory:{retired:true}});
    expect((await call({...push,operation_id:"revive",expected_revision:2})).status).toBe(409);
    await expect(runInDurableObject(stub(cfg),(_,ctx)=>ctx.abort("Sync restart"))).rejects.toThrow();
    expect((await call(push)).data.replayed).toBe(true);
    expect((await call({action:"pull",cursor:0,scopes:["project"]})).data.items[0].memory.retired).toBe(true);
  });
  it("rolls back sync state and receipts together",async()=>{
    const cfg=config(),t=await token();
    await runInDurableObject(stub(cfg),(_,ctx)=>ctx.storage.sql.exec("CREATE TRIGGER sync_failure BEFORE INSERT ON receipts BEGIN SELECT RAISE(ABORT,'sync failure'); END"));
    const {operation_id:_op,...m}=lesson;
    const body={action:"push",operation_id:"sync-fail",expected_revision:0,memory:{...m,id:"x",retired:false,created:"2026-01-01T00:00:00Z",updated:"2026-01-01T00:00:00Z"}};
    expect((await request(cfg,"/v1/sync",body,t)).status).toBe(503);
    expect(await runInDurableObject(stub(cfg),(_,ctx)=>ctx.storage.sql.exec<{n:number}>("SELECT count(*) n FROM sync_state").one().n)).toBe(0);
    await runInDurableObject(stub(cfg),(_,ctx)=>ctx.storage.sql.exec("DROP TRIGGER sync_failure"));
    expect((await request(cfg,"/v1/sync",body,t)).status).toBe(200);
  });
  it("requires membership and an independent reviewer for exact team snapshots",async()=>{
    const cfg={...config(),TEAM_MEMBERS:JSON.stringify({platform:{members:["alice","bob"],reviewers:["bob"]},other:{members:["bob"],reviewers:["bob"]}})};
    const alice=await token(),bob=await token("bob","memory:read memory:write memory:review");
    const id=(await execute(cfg,"record_memory",lesson,alice)).data.result.id;
    const call=async(a:unknown,t=alice)=>{const r=await request(cfg,"/v1/team",a,t);return {status:r.status,data:await r.json() as any}};
    const proposal={action:"propose",operation_id:"proposal-1",team:"platform",memory_id:id};
    expect((await call(proposal,await token("alice","memory:write"))).status).toBe(403);
    const first=await call(proposal);expect(first.status).toBe(200);expect(first.data.proposal.approved).toBe(false);
    expect((await call(proposal)).data.proposal.id).toBe(first.data.proposal.id);
    expect((await call({...proposal,team:"other"})).status).toBe(403);
    const p=first.data.proposal;
    const review={action:"review",operation_id:"review-1",team:"platform",id:p.id,expected_revision:1,digest:p.digest,approve:true};
    expect((await call(review,await token("bob","memory:review"))).status).toBe(403);
    expect((await call(review)).status).toBe(403);
    expect((await call(review,await token("bob"))).status).toBe(403);
    expect((await call({...review,digest:"0".repeat(64)},bob)).status).toBe(409);
    const approved=await call(review,bob);expect(approved.data.proposal).toMatchObject({approved:true,reviewer:"bob",revision:2});
    expect((await call({...review,operation_id:"stale"},bob)).status).toBe(409);
    expect((await call({action:"pull",team:"platform",cursor:0},bob)).data.items[0].approved).toBe(true);
    const retired=await call({action:"retire",operation_id:"withdraw-team",team:"platform",id:p.id,expected_revision:2});
    expect(retired.data.proposal).toMatchObject({retired:true,approved:false,revision:3});
    expect((await call({...review,operation_id:"resurrect",expected_revision:3},bob)).status).toBe(409);
  });
  it("reserves retirement capacity after ordinary receipts are exhausted", async () => {
    const cfg = config(), t = await token();
    const id = (await execute(cfg, "record_memory", lesson, t)).data.result.id;
    await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec("UPDATE metadata SET value='10000' WHERE key='receipt_count'"));
    expect((await execute(cfg, "record_memory", { ...lesson, operation_id: "extra" }, t)).response.status).toBe(422);
    const a = { operation_id: "withdraw", id };
    expect((await execute(cfg, "forget_memory", a, t)).response.status).toBe(200);
    expect((await execute(cfg, "forget_memory", a, t)).data.replayed).toBe(true);
    expect((await execute(cfg, "forget_memory", { ...a, operation_id: "waste-reserve" }, t)).response.status).toBe(422);
    expect((await execute(cfg, "recall_memory", { task: "redis", project_id: "p", context_features: { database: ["redis"] } }, t)).data.result.hits).toEqual([]);
  });
  it("fails closed and validates issuer, audience, expiry, subject and scope", async () => {
    const cfg = config();
    expect((await request(cfg, "/v1/operations", op("recall_memory", {}))).status).toBe(401);
    for (const t of [await token("mallory"), await token("alice", "memory:read", "https://wrong.example"),
      await token("alice", "memory:read", undefined, "wrong"), await token("alice", "memory:read", undefined, undefined, "-1m")]) {
      expect((await request(cfg, "/v1/operations", op("recall_memory", {}), t)).status).toBe(401);
    }
    expect((await request({ ...cfg, AUTH_JWKS: "" }, "/mcp", {}, await token())).status).toBe(503);
    expect((await request(cfg, "/v1/operations", op("record_memory", lesson), await token("alice", "memory:read"))).status).toBe(403);
    expect((await request(cfg, "/mcp", {}, await token(), { Origin: "https://other.example" })).status).toBe(403);
  });
  it("publishes protected-resource metadata without exposing keys or identities", async () => {
    const cfg = config();
    const response = await worker.fetch(new Request(cfg.PUBLIC_ORIGIN + "/.well-known/oauth-protected-resource/mcp"), cfg, createExecutionContext());
    expect(await response.json()).toEqual({ resource: cfg.PUBLIC_ORIGIN + "/mcp", authorization_servers: [cfg.AUTH_ISSUER],
      scopes_supported: ["memory:read", "memory:write"], bearer_methods_supported: ["header"] });
  });
  it("commits receipts atomically, replays retries, and preserves data across forced restart", async () => {
    const cfg = config(), t = await token();
    const first = await execute(cfg, "record_memory", lesson, t);
    expect(first.response.status).toBe(200);
    const id = first.data.result.id;
    const retry = await execute(cfg, "record_memory", lesson, t);
    expect(retry.data.result.id).toBe(id); expect(retry.data.replayed).toBe(true);
    expect((await execute(cfg, "record_memory", { ...lesson, lesson: "Changed lesson." }, t)).response.status).toBe(409);
    await expect(runInDurableObject(stub(cfg), (_, ctx) => ctx.abort("Acceptance restart"))).rejects.toThrow();
    const restored = await execute(cfg, "record_memory", lesson, t);
    expect(restored.data.result.id).toBe(id); expect(restored.data.replayed).toBe(true);
    const recalled = await execute(cfg, "recall_memory", { task: "redis", project_id: "p", context_features: { database: ["redis"] } }, t);
    expect(recalled.data.result.hits.map(h => h.id)).toEqual([id]);
    expect(await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec<{ n: number }>("SELECT count(*) AS n FROM memories").one().n)).toBe(1);
  });
  it("rolls back an interrupted write before acknowledgement and permits safe retry", async () => {
    const cfg = config(), t = await token();
    await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec("CREATE TRIGGER fail_receipt BEFORE INSERT ON receipts BEGIN SELECT RAISE(ABORT,'test failure'); END"));
    expect((await execute(cfg, "record_memory", lesson, t)).response.status).toBe(503);
    expect(await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec<{ n: number }>("SELECT count(*) AS n FROM memories").one().n)).toBe(0);
    await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec("DROP TRIGGER fail_receipt"));
    expect((await execute(cfg, "record_memory", lesson, t)).response.status).toBe(200);
  });
  it("isolates users, projects and unknown requirements", async () => {
    const cfg = config(), t = await token(), bob = await token("bob");
    const saved = await execute(cfg, "record_memory", lesson, t), id = saved.data.result.id;
    for (const [bearer, args] of [[bob, { task: "redis", project_id: "p", context_features: { database: ["redis"] } }],
      [t, { task: "redis", project_id: "other", context_features: { database: ["redis"] } }], [t, { task: "redis", project_id: "p" }]] as const) {
      expect((await execute(cfg, "recall_memory", args, bearer)).data.result.hits).toEqual([]);
    }
    expect((await execute(cfg, "forget_memory", { operation_id: "forget", id }, bob)).response.status).toBe(422);
    expect((await execute(cfg, "record_memory", { ...lesson, owner: "bob" }, t)).response.status).toBe(400);
    expect((await execute(cfg, "record_memory", { ...lesson, scope: "team" }, t)).response.status).toBe(400);
    expect((await request(cfg, "/v1/operations", { version: 2, tool: "recall_memory", arguments: {} }, t)).status).toBe(400);
  });
  it("deduplicates feedback independently of retries and retirement prevents resurrection", async () => {
    const cfg = config(), t = await token();
    const saved = await execute(cfg, "record_memory", lesson, t), id = saved.data.result.id;
    const f = { operation_id: "feedback-1", id, feedback_id: "task-1", helpful: true, project_id: "p" };
    expect((await execute(cfg, "feedback_memory", f, t)).response.status).toBe(200);
    expect((await execute(cfg, "feedback_memory", { ...f, operation_id: "feedback-2" }, t)).response.status).toBe(200);
    expect((await execute(cfg, "feedback_memory", { ...f, operation_id: "feedback-3", helpful: false }, t)).response.status).toBe(409);
    expect(await runInDurableObject(stub(cfg), (_, ctx) => JSON.parse(ctx.storage.sql.exec<{ data: string }>("SELECT data FROM memories").one().data).helpful)).toBe(1);
    expect((await execute(cfg, "forget_memory", { operation_id: "retire", id }, t)).response.status).toBe(200);
    expect((await execute(cfg, "record_memory", lesson, t)).data.replayed).toBe(true);
    expect((await execute(cfg, "recall_memory", { task: "redis", project_id: "p", context_features: { database: ["redis"] } }, t)).data.result.hits).toEqual([]);
  });
  it("serves six MCP tools and recalls after a fresh MCP initialization", async () => {
    const cfg = config(), t = await token();
    const init = { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "acceptance", version: "1" } };
    expect(await rpc(cfg, "initialize", init, t)).toMatchObject({ status: 200, data: { result: { serverInfo: { name: "elephant-cloud" } } } });
    const list = await rpc(cfg, "tools/list", {}, t);
    expect(list.data.result.tools).toHaveLength(6);
    const saved = await rpc(cfg, "tools/call", { name: "record_memory", arguments: lesson }, t);
    expect(saved.data.result.isError).not.toBe(true);
    const id = saved.data.result.structuredContent.result.id;
    await rpc(cfg, "initialize", init, t, 10);
    const recalled = await rpc(cfg, "tools/call", { name: "recall_memory", arguments: { task: "redis", project_id: "p", context_features: { database: ["redis"] } } }, t, 11);
    expect(recalled.data.result.structuredContent.result.hits[0].id).toBe(id);
  });
  it("bounds request bodies before parsing", async () => {
    expect((await request(config(), "/v1/operations", { oversized: "x".repeat(65536) }, await token())).status).toBe(413);
  });
  it("serializes concurrent retries into one memory and one receipt", async () => {
    const cfg = config(), t = await token();
    const results = await Promise.all(Array.from({ length: 5 }, () => execute(cfg, "record_memory", lesson, t)));
    expect(new Set(results.map(r => r.data.result.id)).size).toBe(1);
    expect(results.filter(r => r.data.replayed === false)).toHaveLength(1);
    expect(await runInDurableObject(stub(cfg), (_, ctx) => ctx.storage.sql.exec<{ n: number }>("SELECT count(*) AS n FROM receipts").one().n)).toBe(1);
  });
  it("reports bounded candidates and rejects excessive task terms and UTF-8 text", async () => {
    const cfg = config(), t = await token();
    const saved = await execute(cfg, "record_memory", lesson, t);
    await runInDurableObject(stub(cfg), (_, ctx) => {
      ctx.storage.transactionSync(() => {
        for (let i = 0; i < 129; i++) {
          const m = { ...saved.data.result, id: `extra-${i}` };
          ctx.storage.sql.exec("INSERT INTO memories VALUES(?,?,?,?,?,?,?)", m.id, "project", "p", "", 0, m.updated, JSON.stringify(m));
          ctx.storage.sql.exec("INSERT INTO tokens VALUES('redis',?)", m.id);
        }
      });
    });
    const result = await execute(cfg, "recall_memory", { task: "redis", project_id: "p", context_features: { database: ["redis"] } }, t);
    expect(result.data.result.candidates_truncated).toBe(true);
    expect((await execute(cfg, "recall_memory", { task: Array.from({ length: 33 }, (_, i) => `term${i}`).join(" ") }, t)).response.status).toBe(422);
    expect((await execute(cfg, "record_memory", { ...lesson, operation_id: "oversize", lesson: "அ".repeat(1400) }, t)).response.status).toBe(400);
  });
});
