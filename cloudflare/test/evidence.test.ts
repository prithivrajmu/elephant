import { env } from "cloudflare:workers";
import { createExecutionContext, runInDurableObject } from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, expect, it } from "vitest";
import { exportJWK, generateKeyPair, SignJWT } from "jose";
import worker,{type Env} from "../src/index";
import { setupNetwork } from "@msw/cloudflare";
import { http, HttpResponse } from "msw";
import { evidenceFetch } from "../src/evidence";
import { McpServer } from "@modelcontextprotocol/server";
import { createMcpHandler } from "agents/mcp/server";
import { z } from "zod";
let key:CryptoKey,jwks:string;
beforeAll(async()=>{const k=await generateKeyPair("RS256",{extractable:true});key=k.privateKey;jwks=JSON.stringify({keys:[{...await exportJWK(k.publicKey),kid:"evidence",alg:"RS256"}]})});
const network=setupNetwork();
beforeEach(()=>network.enable());
afterEach(()=>{network.resetHandlers();network.disable()});
const config=()=>({...env as unknown as Env,PUBLIC_ORIGIN:"https://elephant.example",AUTH_ISSUER:"https://issuer.example",AUTH_AUDIENCE:"elephant",AUTH_JWKS:jwks,AUTH_SUBJECTS:'["alice","bob"]',TENANT_ID:crypto.randomUUID(),
 EVIDENCE_CONNECTORS:JSON.stringify({docs:{url:"https://evidence.example/mcp",tool:"lookup",users:["alice"]}}),EVIDENCE_CREDENTIALS:JSON.stringify({alice:{docs:"external-secret-token"}})});
async function token(user="alice",scopes="memory:read evidence:read") {return new SignJWT({scope:scopes}).setProtectedHeader({alg:"RS256",kid:"evidence"}).setIssuer("https://issuer.example").setAudience("elephant").setSubject(user).setIssuedAt().setExpirationTime("1h").sign(key)}
async function read(cfg:Env,args:unknown,bearer?:string) {const r=await worker.fetch(new Request(cfg.PUBLIC_ORIGIN+"/v1/evidence",{method:"POST",headers:{"Content-Type":"application/json",Authorization:`Bearer ${bearer??await token()}`},body:JSON.stringify(args)}),cfg,createExecutionContext());return {status:r.status,data:await r.json() as any}}
const args={action:"read",request_id:"evidence-1",connector:"docs",arguments:{query:"private-query"},byte_budget:4096};
function remote(readOnly=true,text="Redis cache evidence") {
 let calls=0;
 network.use(http.post("https://evidence.example/mcp",async({request})=>{
  expect(request.headers.get("Authorization")).toBe("Bearer external-secret-token");
  const msg=await request.json() as any;let result:any,error:any;
  if(msg.method==="server/discover")error={code:-32601,message:"legacy server"};
  else if(msg.method==="initialize")result={protocolVersion:"2025-06-18",serverInfo:{name:"fixture",version:"1"},capabilities:{tools:{}}};
  else if(msg.method==="tools/list")result={tools:[{name:"lookup",inputSchema:{type:"object",properties:{query:{type:"string"}}},annotations:{readOnlyHint:readOnly,destructiveHint:!readOnly}}]};
  else if(msg.method==="tools/call"){calls++;result={content:[{type:"text",text}]}}
  else result={};
  return msg.id===undefined?new HttpResponse(null,{status:202}):HttpResponse.json({jsonrpc:"2.0",id:msg.id,...(error?{error}:{result})});
 }),http.get("https://evidence.example/mcp",()=>new HttpResponse(null,{status:405})),http.delete("https://evidence.example/mcp",()=>new HttpResponse(null,{status:200})));
 return ()=>calls;
}
it("uses the Agents MCP client, bounds output and persists hashes without content or secrets",async()=>{
 const cfg=config(),calls=remote(true,"👋 Evidence.\n".repeat(2000));
 const first=await read(cfg,args);expect(first.status).toBe(200);expect(calls()).toBe(1);
 expect(first.data).toMatchObject({untrusted:true,content_retained:false,truncated:true,replayed:false});
 expect(new TextEncoder().encode(JSON.stringify(first.data)).byteLength).toBeLessThanOrEqual(args.byte_budget);
 expect(first.data.source).toMatch(/^mcp-evidence:evidence-1#sha256=[a-f0-9]{64}$/);
 const stub=cfg.EVIDENCE!.get(cfg.EVIDENCE!.idFromName(JSON.stringify([cfg.TENANT_ID,"alice"])));
 const persisted=await runInDurableObject(stub,(_,ctx)=>ctx.storage.sql.exec<{payload:string;metadata:string}>("SELECT payload,metadata FROM evidence_receipts").toArray());
 const raw=JSON.stringify(persisted);expect(raw).not.toContain("private-query");expect(raw).not.toContain("external-secret-token");expect(raw).not.toContain("👋");
 expect(await runInDurableObject(stub,(_,ctx)=>ctx.storage.sql.exec("SELECT * FROM cf_agents_mcp_servers").toArray())).toEqual([]);
 await expect(runInDurableObject(stub,(_,ctx)=>ctx.abort("Evidence restart"))).rejects.toThrow();
 const replay=await read(cfg,args);expect(replay.data.replayed).toBe(true);expect(replay.data.text).toBe("");expect(calls()).toBe(1);
 expect((await read(cfg,{...args,arguments:{query:"changed"}})).status).toBe(409);
 expect((await read(cfg,{...args,request_id:"fresh"})).status).toBe(200);expect(calls()).toBe(2);
});
it("requires dedicated scope and configured subject access before contacting external servers",async()=>{
 expect((await read(config(),args,await token("alice","memory:read"))).status).toBe(403);
 expect((await read(config(),args,await token("bob"))).status).toBe(403);
 expect((await read(config(),{...args,connector:"arbitrary-url"})).status).toBe(403);
});
it("negotiates with a real current SDK MCP server",async()=>{
 const cfg=config();let calls=0;
 const handler=createMcpHandler(()=>{
  const server=new McpServer({name:"modern-evidence",version:"1"});
  server.registerTool("lookup",{inputSchema:z.object({query:z.string()}),annotations:{readOnlyHint:true,destructiveHint:false}},async a=>{
   calls++;return {content:[{type:"text" as const,text:`Observed ${a.query}`} ]};
  });return server;
 },{corsOptions:false,allowedHostnames:["evidence.example"]});
 network.use(http.all("https://evidence.example/mcp",async({request})=>{
  const headers=new Headers(request.headers);headers.set("Host","evidence.example");
  return handler(new Request(request.url,{method:request.method,headers,
    body:["GET","HEAD"].includes(request.method)?undefined:await request.arrayBuffer()}),env,createExecutionContext());
 }));
 const result=await read(cfg,args);expect(result.status).toBe(200);expect(calls).toBe(1);expect(result.data.text).toContain("Observed private-query");
});
it("rejects tools that do not declare read-only behavior",async()=>{
 const calls=remote(false);expect((await read(config(),args)).status).toBe(422);expect(calls()).toBe(0);
});
it("rejects redirects and oversized remote responses before SDK parsing",async()=>{
 network.use(http.post("https://bounded.example/mcp",()=>new HttpResponse(null,{status:302,headers:{location:"https://elsewhere.example/mcp"}})));
 await expect(evidenceFetch("https://bounded.example/mcp","token")("https://bounded.example/mcp",{method:"POST"})).rejects.toThrow("redirects");
 network.resetHandlers();network.use(http.post("https://bounded.example/mcp",()=>new HttpResponse("x".repeat(65537))));
 const large=await evidenceFetch("https://bounded.example/mcp")("https://bounded.example/mcp",{method:"POST"});await expect(large.text()).rejects.toThrow("65536");
 await expect(evidenceFetch("https://bounded.example/mcp")("https://other.example/mcp",{method:"POST"})).rejects.toThrow("changed");
});
