import { env } from "cloudflare:workers";
import { createExecutionContext } from "cloudflare:test";
import { beforeAll, describe, expect, it, vi } from "vitest";
import { exportJWK, generateKeyPair, SignJWT } from "jose";
import worker, { type Env } from "../src/index";

let key: CryptoKey, jwks: string;
beforeAll(async () => {
  const keys = await generateKeyPair("RS256", { extractable: true });
  key = keys.privateKey;
  jwks = JSON.stringify({ keys: [{ ...await exportJWK(keys.publicKey), kid: "ratelimit", alg: "RS256" }] });
});
const config = (): Env => ({ ...(env as unknown as Env), PUBLIC_ORIGIN: "https://elephant.example",
  AUTH_ISSUER: "https://issuer.example", AUTH_AUDIENCE: "elephant", AUTH_JWKS: jwks,
  AUTH_SUBJECTS: '["alice","bob"]', TENANT_ID: crypto.randomUUID() });
async function token(subject = "alice") {
  return new SignJWT({ scope: "memory:read" }).setProtectedHeader({ alg: "RS256", kid: "ratelimit" })
    .setIssuer("https://issuer.example").setAudience("elephant").setSubject(subject)
    .setIssuedAt().setExpirationTime("1h").sign(key);
}
async function request(cfg: Env, path = "/v1/operations", bearer?: string, body = JSON.stringify({
  version: 1, tool: "profile_memory", arguments: {},
})) {
  return new Request(cfg.PUBLIC_ORIGIN + path, { method: "POST",
    headers: { Authorization: `Bearer ${bearer ?? await token()}`, "Content-Type": "application/json" }, body });
}
const fetch = (req: Request, cfg: Env) => worker.fetch(req, cfg, createExecutionContext());
function forbidStorage(cfg: Env) {
  const idFromName = vi.fn(() => { throw new Error("Unexpected Durable Object access"); });
  const get = vi.fn(() => { throw new Error("Unexpected Durable Object dispatch"); });
  cfg.MEMORY = { idFromName, get } as unknown as Env["MEMORY"];
  cfg.TEAM = { idFromName, get } as unknown as Env["TEAM"];
  cfg.EVIDENCE = { idFromName, get } as unknown as Env["EVIDENCE"];
  return { idFromName, get };
}

describe("per-subject authenticated request rate limiting", () => {
  it("allows a request with the verified tenant:sub key before reading the body or resolving storage", async () => {
    const cfg = config(), namespace = cfg.MEMORY;
    const limit = vi.fn(async () => ({ success: true }));
    const idFromName = vi.fn((name: string) => namespace.idFromName(name));
    const get = vi.fn((id: DurableObjectId) => namespace.get(id));
    cfg.RATE_LIMITER = { limit };
    cfg.MEMORY = { idFromName, get } as unknown as Env["MEMORY"];
    const req = await request(cfg, "/v1/operations", await token("bob"));
    const reader = vi.spyOn(req.body!, "getReader");
    const response = await fetch(req, cfg);
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({ version: 1, result: {} });
    expect(limit).toHaveBeenCalledExactlyOnceWith({ key: `${cfg.TENANT_ID}:bob` });
    expect(idFromName).toHaveBeenCalledExactlyOnceWith(JSON.stringify([cfg.TENANT_ID, "bob"]));
    expect(limit.mock.invocationCallOrder[0]).toBeLessThan(reader.mock.invocationCallOrder[0]);
    expect(reader.mock.invocationCallOrder[0]).toBeLessThan(idFromName.mock.invocationCallOrder[0]);
    expect(idFromName.mock.invocationCallOrder[0]).toBeLessThan(get.mock.invocationCallOrder[0]);
  });

  it.each(["/mcp", "/v1/operations", "/v1/sync", "/v1/team", "/v1/evidence"])(
    "rejects %s with 429 and Retry-After before body work or any Durable Object access", async path => {
      const cfg = config(), storage = forbidStorage(cfg);
      const limit = vi.fn(async () => ({ success: false }));
      cfg.RATE_LIMITER = { limit };
      // Even an oversized/invalid body must be rejected before boundedRequest.
      const req = await request(cfg, path, await token(), "x".repeat(65537));
      const reader = vi.spyOn(req.body!, "getReader");
      const response = await fetch(req, cfg);
      expect(response.status).toBe(429);
      expect(response.headers.get("Retry-After")).toBe("60");
      expect(response.headers.get("Cache-Control")).toBe("no-store");
      expect(await response.json()).toEqual({ error: "Authenticated request rate limit exceeded" });
      expect(limit).toHaveBeenCalledExactlyOnceWith({ key: `${cfg.TENANT_ID}:alice` });
      expect(reader).not.toHaveBeenCalled();
      expect(storage.idFromName).not.toHaveBeenCalled();
      expect(storage.get).not.toHaveBeenCalled();
    });

  it.each([undefined, "false", "TRUE", "1", ""])("fails closed without a binding when the disable flag is %s", async flag => {
    const cfg = { ...config(), RATE_LIMITER: undefined, RATE_LIMIT_DISABLED: flag };
    const storage = forbidStorage(cfg), req = await request(cfg);
    const reader = vi.spyOn(req.body!, "getReader");
    const response = await fetch(req, cfg);
    expect(response.status).toBe(503);
    expect(await response.json()).toEqual({ error: "Hosted request rate limiting is not configured" });
    expect(reader).not.toHaveBeenCalled();
    expect(storage.idFromName).not.toHaveBeenCalled();
    expect(storage.get).not.toHaveBeenCalled();
  });

  it("permits an explicit local-development opt-out when the binding is absent", async () => {
    const cfg = { ...config(), RATE_LIMITER: undefined, RATE_LIMIT_DISABLED: "true" };
    expect((await fetch(await request(cfg), cfg)).status).toBe(200);
  });

  it("does not bypass a configured limiter even when the disable flag is true", async () => {
    const cfg = { ...config(), RATE_LIMITER: { limit: vi.fn(async () => ({ success: false })) }, RATE_LIMIT_DISABLED: "true" };
    expect((await fetch(await request(cfg), cfg)).status).toBe(429);
    expect(cfg.RATE_LIMITER.limit).toHaveBeenCalledOnce();
  });

  it("fails closed on limiter errors without reading the body or dispatching to storage", async () => {
    const cfg = config(), storage = forbidStorage(cfg);
    cfg.RATE_LIMITER = { limit: vi.fn(async () => { throw new Error("Limiter unavailable"); }) };
    const req = await request(cfg), reader = vi.spyOn(req.body!, "getReader");
    const response = await fetch(req, cfg);
    expect(response.status).toBe(503);
    expect(reader).not.toHaveBeenCalled();
    expect(storage.idFromName).not.toHaveBeenCalled();
    expect(storage.get).not.toHaveBeenCalled();
  });

  it("does not key unauthenticated or non-allowlisted subjects", async () => {
    const cfg = config(), storage = forbidStorage(cfg), limit = vi.fn(async () => ({ success: true }));
    cfg.RATE_LIMITER = { limit };
    for (const bearer of ["", "not-a-jwt", await token("mallory")]) {
      expect((await fetch(await request(cfg, "/v1/operations", bearer), cfg)).status).toBe(401);
    }
    expect(limit).not.toHaveBeenCalled();
    expect(storage.idFromName).not.toHaveBeenCalled();
    expect(storage.get).not.toHaveBeenCalled();
  });

  it("keeps public health and OAuth metadata available without a limiter", async () => {
    const cfg = { ...config(), RATE_LIMITER: undefined };
    for (const path of ["/health", "/.well-known/oauth-protected-resource/mcp"]) {
      expect((await fetch(new Request(cfg.PUBLIC_ORIGIN + path), cfg)).status).toBe(200);
    }
  });
});
