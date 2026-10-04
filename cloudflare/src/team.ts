import { DurableObject } from "cloudflare:workers";
import { z } from "zod";
import { canonical, type Memory, type Principal } from "./contracts";
const id=z.string().min(1).max(128);
export const teamSchema=z.discriminatedUnion("action",[
 z.object({action:z.literal("propose"),operation_id:id,team:id,memory_id:id}).strict(),
 z.object({action:z.literal("review"),operation_id:id,team:id,id,expected_revision:z.number().int().min(1),digest:z.string().regex(/^[a-f0-9]{64}$/),approve:z.boolean()}).strict(),
 z.object({action:z.literal("retire"),operation_id:id,team:id,id,expected_revision:z.number().int().min(1)}).strict(),
 z.object({action:z.literal("pull"),team:id,cursor:z.number().int().min(0)}).strict(),
]);
export interface Proposal {id:string;author:string;revision:number;digest:string;approved:boolean;retired:boolean;memory:Memory;reviewer?:string}
export async function digestMemory(m:Memory) {
 const data=await crypto.subtle.digest("SHA-256",new TextEncoder().encode(canonical(m)));
 return [...new Uint8Array(data)].map(b=>b.toString(16).padStart(2,"0")).join("");
}
export class TeamMemory extends DurableObject {
 constructor(ctx:DurableObjectState,env:Record<string,unknown>) {
  super(ctx,env);ctx.storage.transactionSync(()=>{
   ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS proposals(id TEXT PRIMARY KEY,seq INTEGER NOT NULL,data TEXT NOT NULL)");
   ctx.storage.sql.exec("CREATE INDEX IF NOT EXISTS proposal_seq ON proposals(seq)");
   ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS team_meta(key TEXT PRIMARY KEY,value TEXT NOT NULL)");
   ctx.storage.sql.exec("INSERT OR IGNORE INTO team_meta VALUES('seq','0')");
   ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS team_receipts(id TEXT PRIMARY KEY,payload TEXT NOT NULL,response TEXT NOT NULL)");
  });
 }
 // Internal RPC. Membership and review authority come from the authenticated
 // Worker configuration; neither a token's team claim nor a caller's fields.
 execute(principal:Principal,reviewer:boolean,input:string,snapshot:string):string {
  if(new TextEncoder().encode(input+snapshot).byteLength>65536)throw new Error("Hosted pilot team request too large");
  const a=teamSchema.parse(JSON.parse(input)),sql=this.ctx.storage.sql;
  return JSON.stringify(this.ctx.storage.transactionSync(()=>{
   const binding=canonical([principal.tenant,a.team]);
   const bound=sql.exec<{value:string}>("SELECT value FROM team_meta WHERE key='identity'").toArray()[0];
   if(bound&&bound.value!==binding)throw new Error("Team identity mismatch");
   if(!bound)sql.exec("INSERT INTO team_meta VALUES('identity',?)",binding);
   if(a.action==="pull") {
    const rows=sql.exec<{seq:number;data:string}>("SELECT seq,data FROM proposals WHERE seq>? ORDER BY seq LIMIT 129",a.cursor).toArray(),page=rows.slice(0,128),more=rows.length>128;
    return {version:1,items:page.map(r=>JSON.parse(r.data)),more,cursor:more?page[127].seq:Number(sql.exec<{value:string}>("SELECT value FROM team_meta WHERE key='seq'").one().value)};
   }
   const payload=canonical({principal,a});
   const receipt=sql.exec<{payload:string;response:string}>("SELECT payload,response FROM team_receipts WHERE id=?",a.operation_id).toArray()[0];
   if(receipt){if(receipt.payload!==payload)throw new Error("operation_id conflict");return {...JSON.parse(receipt.response),replayed:true}}
   const receipts=sql.exec<{n:number}>("SELECT count(*) n FROM team_receipts").one().n;
   let p:Proposal;
   if(a.action==="propose") {
    if(receipts>=10000||sql.exec<{n:number}>("SELECT count(*) n FROM proposals").one().n>=1000)throw new Error("Hosted pilot team limit reached");
    const s=JSON.parse(snapshot) as {memory:Memory;digest:string};
    if(!s.memory||s.memory.id!==a.memory_id||s.memory.owner!==principal.user||s.memory.tenant!==principal.tenant||s.memory.retired)throw new Error("Unknown owned active memory");
    p={id:crypto.randomUUID(),author:principal.user,revision:1,digest:s.digest,approved:false,retired:false,memory:s.memory};
   }else {
    const row=sql.exec<{data:string}>("SELECT data FROM proposals WHERE id=?",a.id).toArray()[0];if(!row)throw new Error("Unknown team proposal");p=JSON.parse(row.data);
    if(p.revision!==a.expected_revision)throw new Error("revision conflict");
    if(a.action==="review") {
     if(!reviewer||p.author===principal.user)throw new Error("Independent reviewer required");
     if(receipts>=10000)throw new Error("Hosted pilot team limit reached");
     if(p.retired||a.digest!==p.digest)throw new Error("review digest conflict or retired proposal");
     p.approved=a.approve;p.reviewer=principal.user;
    }else {
     if(p.author!==principal.user&&!reviewer)throw new Error("Only author or reviewer may retire");
     if(p.retired)throw new Error("Team proposal already retired");
     if(receipts>=11000)throw new Error("Hosted pilot team withdrawal limit reached");
     p.retired=true;p.approved=false;
    }
    p.revision++;
   }
   sql.exec("UPDATE team_meta SET value=CAST(value AS INTEGER)+1 WHERE key='seq'");
   const seq=Number(sql.exec<{value:string}>("SELECT value FROM team_meta WHERE key='seq'").one().value);
   sql.exec("INSERT INTO proposals VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET seq=excluded.seq,data=excluded.data",p.id,seq,JSON.stringify(p));
   const response={version:1,proposal:p,replayed:false};sql.exec("INSERT INTO team_receipts VALUES(?,?,?)",a.operation_id,payload,JSON.stringify(response));return response;
  }));
 }
}
