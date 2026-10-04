import { generateKeyPair, exportJWK, SignJWT } from "jose";
import { writeFile } from "node:fs/promises";

// Local development only. No private key is saved or sent to Cloudflare.
const keys = await generateKeyPair("RS256", { extractable: true });
const jwks = { keys: [{ ...await exportJWK(keys.publicKey), kid: "local", alg: "RS256", use: "sig" }] };
const settings = {
  PUBLIC_ORIGIN: "http://127.0.0.1:8787", AUTH_ISSUER: "https://elephant-dev.invalid",
  AUTH_AUDIENCE: "elephant-local", TENANT_ID: "local-pilot",
  AUTH_JWKS: JSON.stringify(jwks), AUTH_SUBJECTS: JSON.stringify(["local-developer"]),
};
const token = await new SignJWT({ scope: "memory:read memory:write" }).setProtectedHeader({ alg: "RS256", kid: "local" })
  .setIssuer(settings.AUTH_ISSUER).setAudience(settings.AUTH_AUDIENCE).setSubject("local-developer")
  .setIssuedAt().setExpirationTime("1h").sign(keys.privateKey);
await writeFile(".dev.vars", Object.entries(settings).map(([k, v]) => `${k}='${v}'`).join("\n") + "\n", { mode: 0o600, flag: "wx" });
await writeFile(".dev-token", token + "\n", { mode: 0o600 });
console.log("Created local .dev.vars and a one-hour token in .dev-token. Keep both private. Start npm run dev.");
