import { DurableObject } from "cloudflare:workers";
import { CONTRACT_VERSION, MAX_CANDIDATES, MAX_MEMORIES, MAX_RECEIPTS, canonical, envelopeSchema,
  toolSchemas, type Envelope, type Memory, type Principal, type RecallArgs, type OperationResponse } from "./contracts";
import { contentWords, visible, recall } from "./recall";
import { installSync, syncOperation } from "./sync";
import { digestMemory } from "./team";

interface Row { [key: string]: SqlStorageValue; data: string }
export class PersonalMemory extends DurableObject {
  constructor(ctx: DurableObjectState, env: Record<string, unknown>) {
    super(ctx, env);
    ctx.storage.transactionSync(() => {
      const sql = ctx.storage.sql;
      sql.exec(`CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY, value TEXT NOT NULL)`);
      const version = sql.exec<{ value: string }>("SELECT value FROM metadata WHERE key='schema_version'").toArray()[0];
      if (version && version.value !== "1") throw new Error("Unsupported hosted storage schema");
      sql.exec("INSERT OR IGNORE INTO metadata VALUES('schema_version','1')");
      sql.exec(`CREATE TABLE IF NOT EXISTS memories(id TEXT PRIMARY KEY, scope TEXT NOT NULL,
        project TEXT NOT NULL, conversation TEXT NOT NULL, retired INTEGER NOT NULL,
        updated TEXT NOT NULL, data TEXT NOT NULL)`);
      sql.exec("CREATE INDEX IF NOT EXISTS memories_scope ON memories(scope,project,conversation,retired,updated)");
      sql.exec(`CREATE TABLE IF NOT EXISTS tokens(token TEXT NOT NULL, memory_id TEXT NOT NULL,
        PRIMARY KEY(token,memory_id)) WITHOUT ROWID`);
      sql.exec(`CREATE TABLE IF NOT EXISTS receipts(operation_id TEXT PRIMARY KEY, payload TEXT NOT NULL, response TEXT NOT NULL)`);
      sql.exec(`CREATE TABLE IF NOT EXISTS feedback(memory_id TEXT NOT NULL, feedback_id TEXT NOT NULL,
        helpful INTEGER NOT NULL, PRIMARY KEY(memory_id,feedback_id)) WITHOUT ROWID`);
      for (const [key, table] of [["memory_count", "memories"], ["receipt_count", "receipts"]]) {
        if (!sql.exec("SELECT value FROM metadata WHERE key=?", key).toArray().length) {
          sql.exec(`INSERT INTO metadata SELECT ?,CAST(count(*) AS TEXT) FROM ${table}`, key);
        }
      }
      installSync(sql);
    });
  }
  execute(principal: Principal, input: string): string {
    if (new TextEncoder().encode(input).byteLength > 65536) throw new Error("Hosted pilot operation exceeds 65536 bytes");
    return JSON.stringify(this.applyOperation(principal, envelopeSchema.parse(JSON.parse(input))));
  }
  sync(principal: Principal, input: string): string {
    if(new TextEncoder().encode(input).byteLength>65536) throw new Error("Hosted pilot operation exceeds 65536 bytes");
    this.execute(principal,JSON.stringify({version:1,tool:"profile_memory",arguments:{}}));
    return JSON.stringify(this.ctx.storage.transactionSync(()=>syncOperation(this.ctx.storage.sql,principal,JSON.parse(input))));
  }
  async evidenceSnapshot(principal:Principal,id:string):Promise<string> {
    this.execute(principal,JSON.stringify({version:1,tool:"profile_memory",arguments:{}}));
    const row=this.ctx.storage.sql.exec<Row>("SELECT data FROM memories WHERE id=?",id).toArray()[0];
    if(!row)throw new Error("Unknown owned active memory");
    const memory=JSON.parse(row.data) as Memory;
    if(memory.retired)throw new Error("Unknown owned active memory");
    return JSON.stringify({memory,digest:await digestMemory(memory)});
  }
  private applyOperation(principal: Principal, input: Envelope): OperationResponse {
    // RPC methods are internal. Validate here as well as at the HTTP boundary.
    const envelope = envelopeSchema.parse(input);
    const args = toolSchemas[envelope.tool].parse(envelope.arguments);
    const sql = this.ctx.storage.sql;
    const identity = canonical(principal);
    const bound = sql.exec<{ value: string }>("SELECT value FROM metadata WHERE key='identity'").toArray()[0];
    if (bound && bound.value !== identity) throw new Error("Identity does not own this object");
    // Identity is derived from verified tokens, never from tool arguments.
    if (!bound) sql.exec("INSERT INTO metadata VALUES('identity',?)", identity);
    const profile = (q: { project_id?: string; conversation_id?: string; context_features?: Record<string, string[]> }) => ({
      project: q.project_id ?? "", conversation: q.conversation_id ?? "", features: q.context_features ?? {},
      evidence: {}, note: "Hosted profile contains client-asserted facts; no local manifests were read.",
    });
    if (envelope.tool === "profile_memory") return { version: CONTRACT_VERSION, result: profile(toolSchemas.profile_memory.parse(args)) };
    if (envelope.tool === "init_memory" || envelope.tool === "recall_memory") {
      const q = args as RecallArgs;
      const terms = [...new Set(contentWords(q.task))];
      if (terms.length > 32) throw new Error("Hosted pilot accepts at most 32 distinct task terms");
      if (!terms.length && (envelope.tool !== "init_memory" || q.task.trim() || !Object.keys(q.context_features ?? {}).length)) {
        return { version: CONTRACT_VERSION, result: { ...recall([], principal, q), candidate_limit: MAX_CANDIDATES,
          candidates_truncated: false, ranking_basis: "bounded hosted candidates" } };
      }
      const scope = "m.retired=0 AND (m.scope='personal' OR (m.scope='project' AND m.project=? AND ?<>'') OR (m.scope='conversation' AND m.conversation=? AND ?<>''))";
      const bindings = [q.project_id ?? "", q.project_id ?? "", q.conversation_id ?? "", q.conversation_id ?? ""];
      const rows = terms.length ? sql.exec<Row>(
        `SELECT DISTINCT m.data FROM tokens t JOIN memories m ON m.id=t.memory_id
         WHERE t.token IN (${terms.map(() => "?").join(",")}) AND ${scope}
         ORDER BY m.updated DESC,m.id LIMIT ?`, ...terms, ...bindings, MAX_CANDIDATES + 1).toArray() :
        sql.exec<Row>(`SELECT m.data FROM memories m WHERE ${scope} ORDER BY m.updated DESC,m.id LIMIT ?`,
          ...bindings, MAX_CANDIDATES + 1).toArray();
      const candidates = rows.slice(0, MAX_CANDIDATES).map(r => JSON.parse(r.data) as Memory);
      return { version: CONTRACT_VERSION, result: { ...recall(candidates, principal, q, envelope.tool === "init_memory"),
        candidate_limit: MAX_CANDIDATES, candidates_truncated: rows.length > MAX_CANDIDATES,
        ranking_basis: "bounded hosted candidates" } };
    }
    const operationID = (args as { operation_id: string }).operation_id;
    const payload = canonical({ version: envelope.version, tool: envelope.tool, arguments: args });
    return this.ctx.storage.transactionSync(() => {
      const old = sql.exec<{ payload: string; response: string }>("SELECT payload,response FROM receipts WHERE operation_id=?", operationID).toArray()[0];
      if (old) {
        if (old.payload !== payload) throw new Error("operation_id was already used with different arguments");
        return { ...JSON.parse(old.response), replayed: true };
      }
      // Reserve one withdrawal receipt for every possible memory. Ordinary
      // mutations cannot consume this capacity, even with distinct retry IDs.
      const exhausted = this.counter("receipt_count") >= MAX_RECEIPTS;
      if (exhausted && envelope.tool !== "forget_memory") throw new Error("Hosted pilot receipt limit reached");
      let result: unknown;
      if (envelope.tool === "record_memory") {
        const a = toolSchemas.record_memory.parse(args);
        if (this.counter("memory_count") >= MAX_MEMORIES) throw new Error("Hosted pilot memory limit reached");
        const tokens = [...new Set(contentWords(`${a.incident} ${a.lesson} ${a.subject ?? ""}`))];
        if (tokens.length > 512) throw new Error("Hosted pilot accepts at most 512 distinct terms per memory");
        const classOutcome: Record<string, string> = { win: "great", lesson: "good", warning: "bad", scar: "worst" };
        const outcomeClass: Record<string, string> = { great: "win", good: "lesson", bad: "warning", worst: "scar" };
        const now = new Date().toISOString();
        const outcome = a.outcome ?? classOutcome[a.class ?? "lesson"];
        const m: Memory = { id: crypto.randomUUID(), ...{ tenant: principal.tenant, owner: principal.user },
          project: a.project_id ?? "", conversation: a.conversation_id ?? "", scope: a.scope,
          approved: false, retired: false, class: a.class ?? outcomeClass[outcome], outcome,
          incident: a.incident, lesson: a.lesson, source: a.source,
          subject: (a.subject ?? "").trim().replace(/\s+/gu, " ").toLowerCase(),
          features: a.features ?? {}, requires: a.requires ?? {}, excludes: a.excludes ?? {},
          created: now, updated: now, helpful: 0, unhelpful: 0 };
        sql.exec("INSERT INTO memories VALUES(?,?,?,?,?,?,?)", m.id, m.scope, m.project, m.conversation, 0, now, JSON.stringify(m));
        for (const t of tokens) sql.exec("INSERT INTO tokens VALUES(?,?)", t, m.id);
        sql.exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='memory_count'");
        result = m;
      } else {
        const a = args as { id: string; feedback_id?: string; helpful?: boolean; project_id?: string; conversation_id?: string };
        const row = sql.exec<Row>("SELECT data FROM memories WHERE id=?", a.id).toArray()[0];
        if (!row) throw new Error("Unknown owned memory");
        const m = JSON.parse(row.data) as Memory;
        if (envelope.tool === "forget_memory") {
          if (exhausted && (m.retired || this.counter("receipt_count") >= MAX_RECEIPTS + MAX_MEMORIES))
            throw new Error("Hosted pilot withdrawal reserve only admits active memories");
          m.retired = true;
        }
        else {
          if (!visible(m, principal, { ...recallSchemaDefaults, project_id: a.project_id, conversation_id: a.conversation_id }))
            throw new Error("Memory is unavailable in this context");
          const previous = sql.exec<{ helpful: number }>("SELECT helpful FROM feedback WHERE memory_id=? AND feedback_id=?", m.id, a.feedback_id!).toArray()[0];
          if (previous) {
            if (Boolean(previous.helpful) !== a.helpful) throw new Error("feedback_id was already used with a different outcome");
          } else {
            sql.exec("INSERT INTO feedback VALUES(?,?,?)", m.id, a.feedback_id!, a.helpful ? 1 : 0);
            if (a.helpful) m.helpful++; else m.unhelpful++;
          }
        }
        // Feedback must not refresh lesson age, matching the local ranking model.
        sql.exec("UPDATE memories SET retired=?,data=? WHERE id=?", m.retired ? 1 : 0, JSON.stringify(m), m.id);
        result = { ok: true };
      }
      const response = { version: CONTRACT_VERSION, operation_id: operationID, replayed: false, result };
      sql.exec("INSERT INTO receipts VALUES(?,?,?)", operationID, payload, JSON.stringify(response));
      sql.exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='receipt_count'");
      return response;
    });
  }
  private counter(key: string) {
    const value = Number(this.ctx.storage.sql.exec<{ value: string }>("SELECT value FROM metadata WHERE key=?", key).one().value);
    if (!Number.isSafeInteger(value) || value < 0) throw new Error("Hosted storage counter is invalid");
    return value;
  }
}
const recallSchemaDefaults: RecallArgs = { task: "", byte_budget: 4000, limit: 6 };
