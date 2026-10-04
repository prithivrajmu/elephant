import { createLocalJWKSet, jwtVerify } from "jose";
import type { Principal } from "./contracts";

export interface AuthConfig {
  PUBLIC_ORIGIN: string;
  AUTH_ISSUER: string;
  AUTH_AUDIENCE: string;
  AUTH_JWKS: string;
  AUTH_SUBJECTS: string;
  TENANT_ID: string;
}
export interface Authenticated { principal: Principal; scopes: Set<string> }
export class AuthError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
export function validateConfig(env: AuthConfig) {
  try {
    const origin = new URL(env.PUBLIC_ORIGIN), issuer = new URL(env.AUTH_ISSUER);
    const local = ["localhost", "127.0.0.1", "[::1]"].includes(origin.hostname);
    if ((!local && origin.protocol !== "https:") || !["http:", "https:"].includes(origin.protocol) ||
      origin.origin !== env.PUBLIC_ORIGIN || origin.username || origin.password ||
      issuer.protocol !== "https:" || issuer.username || issuer.password || issuer.hash || issuer.search ||
      !env.AUTH_AUDIENCE || !env.TENANT_ID || env.TENANT_ID.length > 256) throw new Error();
    const subjects: unknown = JSON.parse(env.AUTH_SUBJECTS);
    if (!Array.isArray(subjects) || !subjects.length || subjects.length > 100 || subjects.some(s => typeof s !== "string" || !s.trim() || s.length > 256)) throw new Error();
    const jwks = JSON.parse(env.AUTH_JWKS);
    if (!Array.isArray(jwks.keys) || !jwks.keys.length || jwks.keys.length > 8 || jwks.keys.some((k: Record<string, unknown>) => k.kty !== "RSA" || k.d || k.p || k.q)) throw new Error();
    return { origin, subjects: subjects as string[], jwks };
  } catch { throw new AuthError(503, "Hosted authentication is not configured"); }
}
export async function authenticate(request: Request, env: AuthConfig): Promise<Authenticated> {
  const config = validateConfig(env);
  const url = new URL(request.url);
  if (url.origin !== config.origin.origin) throw new AuthError(403, "Unexpected endpoint origin");
  const origin = request.headers.get("Origin");
  if (origin && origin !== config.origin.origin) throw new AuthError(403, "Unexpected browser origin");
  const authorization = request.headers.get("Authorization") ?? "";
  if (!authorization.startsWith("Bearer ") || authorization.length > 16384) throw new AuthError(401, "Bearer access token required");
  try {
    const { payload } = await jwtVerify(authorization.slice(7), createLocalJWKSet(config.jwks), {
      issuer: env.AUTH_ISSUER, audience: env.AUTH_AUDIENCE, algorithms: ["RS256"],
      requiredClaims: ["sub", "exp", "iat"], maxTokenAge: "1h", clockTolerance: 5,
    });
    if (!payload.sub || !config.subjects.includes(payload.sub) || typeof payload.scope !== "string") throw new Error();
    return { principal: { tenant: env.TENANT_ID, user: payload.sub }, scopes: new Set(payload.scope.split(" ")) };
  } catch { throw new AuthError(401, "Invalid or unauthorized access token"); }
}
