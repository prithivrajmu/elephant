import { z } from "zod";
import { canonical, MAX_MEMORIES, MAX_RECEIPTS, recordSchema, type Memory, type Principal } from "./contracts";
import { contentWords } from "./recall";
const {operation_id: _operationID,...memoryShape}=recordSchema.shape;
export const syncMemorySchema = z.object({...memoryShape,id:z.string().min(1).max(128),retired:z.boolean(),created:z.string().datetime({offset:true}),updated:z.string().datetime({offset:true})}).strict().superRefine((v,ctx)=>{
 if(v.scope==="project"&&!v.project_id) ctx.addIssue({code:"custom",message:"Project scope requires project_id"});
 if(v.scope==="conversation"&&!v.conversation_id) ctx.addIssue({code:"custom",message:"Conversation scope requires conversation_id"});
});
export const syncSchema = z.discriminatedUnion("action", [
  z.object({action:z.literal("push"),operation_id:z.string().min(1).max(128),expected_revision:z.number().int().min(0),memory:syncMemorySchema}).strict(),
  z.object({action:z.literal("pull"),cursor:z.number().int().min(0),scopes:z.array(z.enum(["personal","project","conversation"])).min(1).max(3)}).strict(),
]);
export function installSync(sql:SqlStorage) {
  sql.exec("INSERT OR IGNORE INTO metadata VALUES('sync_seq','0')");
  sql.exec("CREATE TABLE IF NOT EXISTS sync_state(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,seq INTEGER NOT NULL)");
  sql.exec("CREATE INDEX IF NOT EXISTS sync_sequence ON sync_state(seq)");
  // Backfill old pilot rows with unique sequence numbers, once per row.
  for(const r of sql.exec<{id:string}>("SELECT id FROM memories WHERE id NOT IN (SELECT id FROM sync_state) ORDER BY id").toArray()) {
    sql.exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='sync_seq'");
    sql.exec("INSERT INTO sync_state SELECT ?,1,CAST(value AS INTEGER) FROM metadata WHERE key='sync_seq'",r.id);
  }
  for(const event of ["INSERT","UPDATE"]) sql.exec(`CREATE TRIGGER IF NOT EXISTS sync_${event.toLowerCase()} AFTER ${event} ON memories BEGIN
    UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='sync_seq';
    INSERT INTO sync_state VALUES(NEW.id,1,(SELECT CAST(value AS INTEGER) FROM metadata WHERE key='sync_seq'))
    ON CONFLICT(id) DO UPDATE SET revision=revision+1,seq=excluded.seq; END`);
}
export function syncOperation(sql:SqlStorage,principal:Principal,input:unknown) {
  const a=syncSchema.parse(input);
  if(a.action==="pull") {
    const rows=sql.exec<{data:string;revision:number;seq:number}>(`SELECT m.data,s.revision,s.seq FROM sync_state s JOIN memories m ON m.id=s.id
      WHERE s.seq>? AND m.scope IN (${a.scopes.map(()=>"?").join(",")}) ORDER BY s.seq LIMIT 129`,a.cursor,...a.scopes).toArray();
    const page=rows.slice(0,128), more=rows.length>128;
    const cursor=more?page[127].seq:Number(sql.exec<{value:string}>("SELECT value FROM metadata WHERE key='sync_seq'").one().value);
    return {version:1,cursor,more,items:page.map(r=>({revision:r.revision,memory:JSON.parse(r.data) as Memory}))};
  }
  const payload=canonical({protocol:"sync-v1",...a});
  const prior=sql.exec<{payload:string;response:string}>("SELECT payload,response FROM receipts WHERE operation_id=?",a.operation_id).toArray()[0];
  if(prior) {if(prior.payload!==payload) throw new Error("operation_id conflict");return {...JSON.parse(prior.response),replayed:true}}
  const count=Number(sql.exec<{value:string}>("SELECT value FROM metadata WHERE key='receipt_count'").one().value);
  const row=sql.exec<{data:string;revision:number}>("SELECT m.data,s.revision FROM memories m JOIN sync_state s ON s.id=m.id WHERE m.id=?",a.memory.id).toArray()[0];
  if((row?.revision??0)!==a.expected_revision) throw new Error("revision conflict; pull and review before retrying");
  const old=row?JSON.parse(row.data) as Memory:undefined;
  if(old&&(old.scope!==a.memory.scope||old.project!==(a.memory.project_id??"")||old.conversation!==(a.memory.conversation_id??"")))
    throw new Error("scope/context conflict; use a new memory ID");
  if(old?.retired&&!a.memory.retired) throw new Error("retired memory cannot be resurrected");
  const withdrawal=!!old&&!old.retired&&a.memory.retired;
  if(count>=MAX_RECEIPTS&&(!withdrawal||count>=MAX_RECEIPTS+MAX_MEMORIES)) throw new Error("Hosted pilot receipt limit reached");
  if(!old && Number(sql.exec<{value:string}>("SELECT value FROM metadata WHERE key='memory_count'").one().value)>=MAX_MEMORIES) throw new Error("Hosted pilot memory limit reached");
  const v=a.memory,now=new Date().toISOString();
  const classOutcome:Record<string,string>={win:"great",lesson:"good",warning:"bad",scar:"worst"}, outcomeClass:Record<string,string>={great:"win",good:"lesson",bad:"warning",worst:"scar"};
  const outcome=v.outcome??classOutcome[v.class??"lesson"];
  const m:Memory={id:v.id,tenant:principal.tenant,owner:principal.user,project:v.project_id??"",conversation:v.conversation_id??"",scope:v.scope,
    approved:false,retired:v.retired,class:v.class??outcomeClass[outcome],outcome,incident:v.incident,lesson:v.lesson,source:v.source,
    subject:(v.subject??"").trim().replace(/\s+/gu," ").toLowerCase(),features:v.features??{},requires:v.requires??{},excludes:v.excludes??{},
    created:old?.created??v.created,updated:v.updated,helpful:old?.helpful??0,unhelpful:old?.unhelpful??0};
  const tokens=[...new Set(contentWords(`${m.incident} ${m.lesson} ${m.subject}`))];
  if(tokens.length>512) throw new Error("Hosted pilot accepts at most 512 distinct terms per memory");
  sql.exec(`INSERT INTO memories VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET scope=excluded.scope,project=excluded.project,
    conversation=excluded.conversation,retired=excluded.retired,updated=excluded.updated,data=excluded.data`,m.id,m.scope,m.project,m.conversation,m.retired?1:0,m.updated,JSON.stringify(m));
  sql.exec("DELETE FROM tokens WHERE memory_id=?",m.id);for(const t of tokens) sql.exec("INSERT INTO tokens VALUES(?,?)",t,m.id);
  if(!old) sql.exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='memory_count'");
  const revision=sql.exec<{revision:number}>("SELECT revision FROM sync_state WHERE id=?",m.id).one().revision;
  const response={version:1,revision,memory:m,replayed:false};
  sql.exec("INSERT INTO receipts VALUES(?,?,?)",a.operation_id,payload,JSON.stringify(response));
  sql.exec("UPDATE metadata SET value=CAST(value AS INTEGER)+1 WHERE key='receipt_count'");return response;
}
