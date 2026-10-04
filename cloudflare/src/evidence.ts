import { DurableObject } from "cloudflare:workers";
import { Lifecycle } from "agents/lifecycle";
import { MCPClientManager } from "agents/mcp/client";
import { z } from "zod";
import { bytes, canonical, type Principal } from "./contracts";
const identifier=z.string().regex(/^[a-zA-Z0-9_-]{1,64}$/);
export const evidenceSchema=z.object({action:z.literal("read"),request_id:identifier,connector:identifier,
 arguments:z.record(z.string(),z.unknown()).refine(a=>bytes(JSON.stringify(a))<=16384,"Evidence arguments exceed 16384 bytes"),
 byte_budget:z.number().int().min(4096).max(16384).default(8192)}).strict();
const registrySchema=z.record(identifier,z.object({url:z.string().max(2048).url(),tool:z.string().min(1).max(128),users:z.array(z.string().min(1).max(256)).min(1).max(100)}).strict())
 .refine(v=>Object.keys(v).length<=8,"Maximum eight evidence connectors");
export interface EvidenceEnv {EVIDENCE_CONNECTORS?:string;EVIDENCE_CREDENTIALS?:string}
export function connectorConfig(env:EvidenceEnv,user:string,id:string) {
 const registry=registrySchema.parse(JSON.parse(env.EVIDENCE_CONNECTORS??"{}")),c=registry[id];
 if(!c||!c.users.includes(user))throw new Error("Evidence connector is not authorized");
 const u=new URL(c.url);
 if(u.protocol!=="https:"||u.username||u.password||u.search||u.hash||u.hostname==="localhost"||u.hostname.endsWith(".local")||u.hostname.endsWith(".internal")||/^\[|^[\d.]+$/.test(u.hostname))throw new Error("Evidence connector URL must be a configured public HTTPS endpoint");
 const credentials=JSON.parse(env.EVIDENCE_CREDENTIALS??"{}");const token=credentials[user]?.[id];
 if(token!==undefined&&(typeof token!=="string"||!token||token.length>8192||/[\r\n]/.test(token)))throw new Error("Evidence credential is not configured");
 return {...c,token:token as string|undefined};
}
async function hash(value:string) {
 const h=await crypto.subtle.digest("SHA-256",new TextEncoder().encode(value));return [...new Uint8Array(h)].map(b=>b.toString(16).padStart(2,"0")).join("");
}
// Pin every request to the configured endpoint, reject redirects, and bound
// response bytes before the MCP SDK parses discovery or tool responses.
export function evidenceFetch(url:string,token?:string):typeof fetch {
 return async(input:RequestInfo|URL,init?:RequestInit)=>{
  const req=new Request(input,init);if(req.url!==url)throw new Error("Evidence endpoint changed");
  const headers=new Headers(req.headers);if(token)headers.set("Authorization",`Bearer ${token}`);
  const response=await fetch(new Request(req,{headers,redirect:"manual",signal:AbortSignal.any([req.signal,AbortSignal.timeout(10000)])}));
  if(response.status>=300&&response.status<400)throw new Error("Evidence redirects are not allowed");
  if(!response.body)return response;
  let n=0;
  const bounded=response.body.pipeThrough(new TransformStream<Uint8Array,Uint8Array>({transform(chunk,controller){
   n+=chunk.byteLength;if(n>65536)throw new Error("Evidence response exceeds 65536 bytes");controller.enqueue(chunk);
  }}));
  return new Response(bounded,{status:response.status,statusText:response.statusText,headers:response.headers});
 };
}
function boundedResult(metadata:Record<string,unknown>,text:string,budget:number) {
 const out={version:1,...metadata,text,untrusted:true,content_retained:false,truncated:false,bytes:0,byte_budget:budget};
 if(bytes(JSON.stringify({...out,text:""}))>budget-20)throw new Error("Evidence budget cannot fit provenance");
 let lo=0,hi=text.length;
 while(lo<hi){const mid=Math.ceil((lo+hi)/2);if(bytes(JSON.stringify({...out,text:text.slice(0,mid)}))<=budget-20)lo=mid;else hi=mid-1}
 if(lo>0&&/[\uD800-\uDBFF]/.test(text[lo-1]))lo--;
 out.text=text.slice(0,lo);out.truncated=lo<text.length;
 out.bytes=bytes(JSON.stringify(out));out.bytes=bytes(JSON.stringify(out));return out;
}
export class EvidenceConnector extends DurableObject<EvidenceEnv> {
 readonly mcp=new MCPClientManager("elephant-evidence","0.1.0");
 readonly lifecycle=Lifecycle.install(this).use(this.mcp);
 private serial:Promise<unknown>=Promise.resolve();
 constructor(ctx:DurableObjectState,env:EvidenceEnv){super(ctx,env);ctx.storage.transactionSync(()=>{
  ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS evidence_identity(value TEXT NOT NULL)");
  ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS evidence_receipts(id TEXT PRIMARY KEY,payload TEXT NOT NULL,metadata TEXT NOT NULL)");
 })}
 read(principal:Principal,input:string,configuration:string):Promise<string> {
  // Serialize network lifecycles so SDK connection cleanup cannot race another
  // read. A crashed remote read can be retried; no external mutation is allowed.
  const job=this.serial.then(()=>this.readOnce(principal,input,configuration));this.serial=job.catch(()=>{});return job;
 }
 private async readOnce(principal:Principal,input:string,configuration:string):Promise<string> {
  if(bytes(input)>65536)throw new Error("Evidence request too large");const a=evidenceSchema.parse(JSON.parse(input));
  // Configuration is supplied through internal RPC by the authenticated Worker,
  // never by MCP/HTTP arguments. Tokens remain in the live fetch closure.
  const selected=JSON.parse(configuration) as {url:string;tool:string;users:string[];token?:string};
  const config=connectorConfig({EVIDENCE_CONNECTORS:JSON.stringify({[a.connector]:{url:selected.url,tool:selected.tool,users:selected.users}}),
    EVIDENCE_CREDENTIALS:JSON.stringify({[principal.user]:{[a.connector]:selected.token}})},principal.user,a.connector),sql=this.ctx.storage.sql,identity=canonical(principal);
  const bound=sql.exec<{value:string}>("SELECT value FROM evidence_identity").toArray()[0];if(bound&&bound.value!==identity)throw new Error("Evidence identity mismatch");
  if(!bound)sql.exec("INSERT INTO evidence_identity VALUES(?)",identity);
  const payload=await hash(canonical({connector:a.connector,url:config.url,tool:config.tool,arguments:a.arguments}));
  const prior=sql.exec<{payload:string;metadata:string}>("SELECT payload,metadata FROM evidence_receipts WHERE id=?",a.request_id).toArray()[0];
  if(prior){if(prior.payload!==payload)throw new Error("Evidence request_id conflict");return JSON.stringify(boundedResult({...JSON.parse(prior.metadata),replayed:true},"",a.byte_budget))}
  if(sql.exec<{n:number}>("SELECT count(*) n FROM evidence_receipts").one().n>=1000)throw new Error("Evidence pilot receipt limit reached");
  await this.lifecycle.start();
  const serverID=crypto.randomUUID();
  try {
   await this.mcp.registerServer(serverID,{name:a.connector,url:config.url,transport:{type:"streamable-http",fetch:evidenceFetch(config.url,config.token)}});
   const connected=await this.mcp.connectToServer(serverID);if(connected.state!=="connected")throw new Error("Evidence connection requires separately authorized credentials");
   const discovered=await this.mcp.discoverIfConnected(serverID,{timeoutMs:10000});if(!discovered?.success)throw new Error("Evidence discovery failed");
   const tool=this.mcp.listTools({serverId:serverID}).find(t=>t.name===config.tool);
   if(!tool||tool.annotations?.readOnlyHint!==true||tool.annotations?.destructiveHint===true)throw new Error("Evidence tool must be explicitly read-only");
   const result=await this.mcp.callTool({serverId:serverID,name:config.tool,arguments:a.arguments},{timeout:10000,maxTotalTimeout:10000});
   if(result.isError)throw new Error("Evidence tool failed");
   const raw=canonical(result);if(bytes(raw)>65536)throw new Error("Evidence result exceeds 65536 bytes");
   const sha256=await hash(raw),arguments_sha256=await hash(canonical(a.arguments));
   const metadata={id:a.request_id,source:`mcp-evidence:${a.request_id}#sha256=${sha256}`,connector:a.connector,url:config.url,tool:config.tool,
    sha256,arguments_sha256,retrieved_at:new Date().toISOString(),replayed:false};
   // Retain only provenance and hashes. Arguments can contain secrets, so the
   // collision receipt stores their hash rather than their raw values.
   const receiptPayload=payload;
   const text=JSON.stringify(result);
   const output=boundedResult(metadata,text,a.byte_budget);
   sql.exec("INSERT INTO evidence_receipts VALUES(?,?,?)",a.request_id,receiptPayload,JSON.stringify(metadata));
   return JSON.stringify(output);
  }finally {await this.mcp.removeServer(serverID)}
 }
}
