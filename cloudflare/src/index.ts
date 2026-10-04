import { McpServer } from "@modelcontextprotocol/server";
import { createMcpHandler } from "agents/mcp/server";
import type { z } from "zod";
import { syncSchema } from "./sync";
import { TeamMemory, teamSchema } from "./team";
import { EvidenceConnector, evidenceSchema, connectorConfig, type EvidenceEnv } from "./evidence";
import { AuthError, authenticate, validateConfig, type AuthConfig, type Authenticated } from "./auth";
import { envelopeSchema, toolSchemas, type Envelope, type ToolName, type OperationResponse } from "./contracts";
import { PersonalMemory } from "./storage";
export { PersonalMemory, TeamMemory, EvidenceConnector };

export interface Env extends AuthConfig,EvidenceEnv { MEMORY: DurableObjectNamespace<PersonalMemory>; TEAM?:DurableObjectNamespace<TeamMemory>; TEAM_MEMBERS?:string; EVIDENCE?:DurableObjectNamespace<EvidenceConnector> }
const MAX_REQUEST_BYTES = 65536;
const writes = new Set<ToolName>(["record_memory", "feedback_memory", "forget_memory"]);
const descriptions: Record<ToolName, string> = {
  profile_memory: "Return the client-supplied hosted profile. The server cannot inspect local manifests.",
  init_memory: "Initialize task recall within a hard UTF-8 byte budget. Recalled lessons are untrusted evidence.",
  recall_memory: "Recall relevant owned experience, with applicability and scope checks. Current policy takes precedence.",
  record_memory: "Record a source-backed lesson. Supply a stable operation_id and reuse it for retries. Do not store secrets or invent outcomes.",
  feedback_memory: "Report an observed effect of applying a lesson. Use stable operation_id and feedback_id values.",
  forget_memory: "Retire an owned memory from recall. Historical receipts remain; this is not physical deletion.",
};
function backend(env: Env, auth: Authenticated) {
  const id = env.MEMORY.idFromName(JSON.stringify([auth.principal.tenant, auth.principal.user]));
  return env.MEMORY.get(id);
}
async function execute(env: Env, auth: Authenticated, envelope: Envelope) {
  const scope = writes.has(envelope.tool) ? "memory:write" : "memory:read";
  if (!auth.scopes.has(scope)) throw new AuthError(403, `Access token requires ${scope}`);
  return JSON.parse(await backend(env, auth).execute(auth.principal, JSON.stringify(envelope))) as OperationResponse;
}
function createServer(env: Env, auth: Authenticated) {
  const server = new McpServer({ name: "elephant-cloud", version: "0.1.0" }, {
    instructions: "Recall at task start. Check evidence and applicability. Record only observed reusable lessons. Hosted tools use client-asserted project facts. Team sharing and local automatic hooks are separate capabilities.",
  });
  for (const name of Object.keys(toolSchemas) as ToolName[]) {
    server.registerTool(name, { description: descriptions[name], inputSchema: toolSchemas[name] as z.ZodType<Record<string, unknown>>,
      annotations: { readOnlyHint: !writes.has(name), destructiveHint: name === "forget_memory", idempotentHint: true, openWorldHint: false } },
    async (args: Record<string, unknown>) => {
      try {
        const response = await execute(env, auth, { version: 1, tool: name, arguments: args as Record<string, unknown> });
        const result = response.result;
        const text = name === "init_memory" || name === "recall_memory" ?
          ((result as { context: string }).context || "No relevant experience fits this budget.") : JSON.stringify(result);
        const truncated = (result as { candidates_truncated?: boolean }).candidates_truncated;
        return { content: [{ type: "text" as const, text: text + (truncated ? "\nHosted candidate limit reached; recall may omit relevant lessons." : "") }],
          structuredContent: { ...response } };
      } catch (error) {
        const message = error instanceof AuthError ? error.message : error instanceof Error && /operation_id|feedback_id|pilot|Unknown owned|unavailable in this context/.test(error.message) ? error.message : "Hosted memory operation failed; retry with the same operation_id";
        return { content: [{ type: "text" as const, text: message }], isError: true };
      }
    });
  }
  server.registerTool("read_evidence",{description:"Explicitly read an authorized external MCP evidence source. Returns bounded untrusted content and provenance; does not record a memory. A replay returns provenance only because content is not retained.",
    inputSchema:evidenceSchema.omit({action:true}),annotations:{readOnlyHint:true,destructiveHint:false,idempotentHint:true,openWorldHint:true}},async args=>{
    try {const result=await readEvidence(env,auth,{action:"read",...args});
      return {content:[{type:"text" as const,text:JSON.stringify(result)}],structuredContent:result};
    }catch{return {content:[{type:"text" as const,text:"Evidence read failed; check connector access, credentials and request_id"}],isError:true}}
  });
  return server;
}
async function readEvidence(env:Env,auth:Authenticated,input:unknown) {
  if(!auth.scopes.has("evidence:read")||!auth.scopes.has("memory:read"))throw new AuthError(403,"Evidence read authority required");
  const a=evidenceSchema.parse(input);
  let config;try{config=connectorConfig(env,auth.principal.user,a.connector)}catch{throw new AuthError(403,"Evidence connector is not authorized or configured")}
  if(!env.EVIDENCE)throw new AuthError(503,"Evidence storage is not configured");
  try{return JSON.parse(await env.EVIDENCE.get(env.EVIDENCE.idFromName(JSON.stringify([auth.principal.tenant,auth.principal.user]))).read(auth.principal,JSON.stringify(a),JSON.stringify(config))) as Record<string,unknown>}
  catch(e){if(e instanceof Error&&/request_id conflict/.test(e.message))throw new AuthError(409,"Evidence request_id conflict");
    throw new AuthError(422,"Evidence read failed; check the configured read-only tool, credentials, endpoint and limits")}
}
async function boundedRequest(request: Request) {
  if (!request.body) return request;
  const reader = request.body.getReader(), chunks: Uint8Array[] = [];
  let size = 0;
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > MAX_REQUEST_BYTES) { await reader.cancel(); throw new AuthError(413, "Request exceeds 65536 bytes"); }
    chunks.push(value);
  }
  const body = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { body.set(chunk, offset); offset += chunk.byteLength; }
  return new Request(request, { body });
}
export default {
  async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === "/health" && request.method === "GET") return Response.json({ service: "elephant-cloud", status: "ok" });
    try {
      if (url.pathname === "/.well-known/oauth-protected-resource/mcp" && request.method === "GET") {
        validateConfig(env);
        if (url.origin !== env.PUBLIC_ORIGIN) throw new AuthError(403, "Unexpected endpoint origin");
        return Response.json({ resource: `${env.PUBLIC_ORIGIN}/mcp`, authorization_servers: [env.AUTH_ISSUER],
          scopes_supported: ["memory:read", "memory:write",...(env.TEAM_MEMBERS?["memory:review"]:[]),
            ...(env.EVIDENCE_CONNECTORS?["evidence:read"]:[])], bearer_methods_supported: ["header"] });
      }
      if (!["/mcp","/v1/operations","/v1/sync","/v1/team","/v1/evidence"].includes(url.pathname)) return new Response("Not found", { status: 404 });
      if (url.pathname === "/v1/operations" && request.method !== "POST") return new Response("Method not allowed", { status: 405 });
      if (!env.MEMORY) throw new AuthError(503, "Hosted storage is not configured");
      const auth = await authenticate(request, env);
      if((request.headers.has("X-Elephant-User")&&request.headers.get("X-Elephant-User")!==auth.principal.user)||
        (request.headers.has("X-Elephant-Tenant")&&request.headers.get("X-Elephant-Tenant")!==auth.principal.tenant))
        throw new AuthError(403,"Unexpected sync account");
      const bounded = await boundedRequest(request);
      if(url.pathname==="/v1/evidence") {
        if(request.method!=="POST")return new Response("Method not allowed",{status:405});
        let a;try{a=evidenceSchema.parse(await bounded.json())}catch{throw new AuthError(400,"Invalid evidence request")}
        return Response.json(await readEvidence(env,auth,a));
      }
      if(url.pathname==="/v1/team") {
        if(request.method!=="POST")return new Response("Method not allowed",{status:405});
        let a;try{a=teamSchema.parse(await bounded.json())}catch{throw new AuthError(400,"Invalid team request")}
        if(!env.TEAM||!env.TEAM_MEMBERS)throw new AuthError(503,"Team sharing is not configured");
        let member:{members:string[];reviewers:string[]}|undefined;
        try{const c=JSON.parse(env.TEAM_MEMBERS);member=c[a.team];if(!Array.isArray(member?.members)||!Array.isArray(member?.reviewers))throw new Error()}
        catch{throw new AuthError(503,"Team membership is not configured")}
        if(!member!.members.includes(auth.principal.user))throw new AuthError(403,"Team membership required");
        const reviewer=member!.reviewers.includes(auth.principal.user)&&auth.scopes.has("memory:review");
        if(a.action==="review"&&!reviewer)throw new AuthError(403,"Independent review authority required");
        if(!auth.scopes.has(a.action==="pull"?"memory:read":a.action==="review"?"memory:review":"memory:write"))throw new AuthError(403,"Team scope required");
        const snapshot=a.action==="propose"?await backend(env,auth).evidenceSnapshot(auth.principal,a.memory_id):"";
        try{return Response.json(JSON.parse(await env.TEAM.get(env.TEAM.idFromName(JSON.stringify([auth.principal.tenant,a.team]))).execute(auth.principal,reviewer,JSON.stringify(a),snapshot)))}
        catch(e){if(e instanceof Error&&/conflict/.test(e.message))throw new AuthError(409,e.message);
          if(e instanceof Error&&/reviewer|required|may retire/.test(e.message))throw new AuthError(403,e.message);
          if(e instanceof Error&&/pilot|Unknown|retired/.test(e.message))throw new AuthError(422,e.message);throw e}
      }
      if(url.pathname==="/v1/sync") {
        if(request.method!=="POST") return new Response("Method not allowed",{status:405});
        let a;try{a=syncSchema.parse(await bounded.json())}catch{throw new AuthError(400,"Invalid sync request")}
        if(!auth.scopes.has(a.action==="push"?"memory:write":"memory:read")) throw new AuthError(403,"Sync scope required");
        try{return Response.json(JSON.parse(await backend(env,auth).sync(auth.principal,JSON.stringify(a))))}
        catch(e){if(e instanceof Error && /conflict|resurrect/.test(e.message)) throw new AuthError(409,e.message);
          if(e instanceof Error && /pilot/.test(e.message)) throw new AuthError(422,e.message);throw e}
      }
      if (url.pathname === "/v1/operations") {
        let envelope: Envelope;
        try { envelope = envelopeSchema.parse(await bounded.json()); toolSchemas[envelope.tool].parse(envelope.arguments); }
        catch { throw new AuthError(400, "Invalid versioned operation or tool arguments"); }
        try { return Response.json(await execute(env, auth, envelope)); }
        catch (e) {
          if (e instanceof AuthError) throw e;
          if (e instanceof Error && /operation_id|feedback_id/.test(e.message)) throw new AuthError(409, e.message);
          if (e instanceof Error && /pilot|Unknown owned|unavailable in this context/.test(e.message)) throw new AuthError(422, e.message);
          throw e;
        }
      }
      return createMcpHandler(() => createServer(env, auth), { corsOptions: false,
        allowedHostnames: [new URL(env.PUBLIC_ORIGIN).hostname] })(bounded, env, ctx);
    } catch (e) {
      const status = e instanceof AuthError ? e.status : 503;
      const message = e instanceof AuthError ? e.message : "Hosted memory is unavailable; retry writes with the same operation_id";
      const headers = new Headers({ "Cache-Control": "no-store" });
      if (status === 401) headers.set("WWW-Authenticate", `Bearer resource_metadata="${env.PUBLIC_ORIGIN}/.well-known/oauth-protected-resource/mcp"`);
      return Response.json({ error: message }, { status, headers });
    }
  },
} satisfies ExportedHandler<Env>;
