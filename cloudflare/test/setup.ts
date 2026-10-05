import { env } from "cloudflare:workers";

// Lifecycle tests exercise storage, not wall-clock-dependent limiter state.
// Rate-limit tests replace this binding per request with deterministic outcomes.
const limiter: RateLimit = { async limit() { return { success: true }; } };
Object.assign(env, { RATE_LIMITER: limiter });
