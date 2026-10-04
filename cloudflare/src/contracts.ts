import { z } from "zod";

export const CONTRACT_VERSION = 1;
export const MAX_CANDIDATES = 128;
export const MAX_MEMORIES = 1000;
export const MAX_RECEIPTS = 10000;
export const bytes = (s: string) => new TextEncoder().encode(s).byteLength;
const text = (max: number) => z.string().refine(s => s.trim().length > 0 && bytes(s) <= max,
  { message: `Expected nonempty text of at most ${max} UTF-8 bytes` });
const optionalID = text(256).optional();
export const labelsSchema = z.record(text(128), z.array(text(128)).min(1).max(32))
  .refine(v => Object.keys(v).length <= 32, "Maximum 32 dimensions");
const context = {
  project_id: optionalID,
  conversation_id: optionalID,
  context_features: labelsSchema.optional(),
};
export const profileSchema = z.object(context).strict();
export const recallSchema = profileSchema.extend({
  task: z.string().refine(s => bytes(s) <= 4096, "Task exceeds 4096 bytes").default(""),
  byte_budget: z.number().int().min(1).max(64000).default(4000),
  limit: z.number().int().min(1).max(20).default(6),
}).strict();
export const recordSchema = z.object({
  operation_id: text(128),
  project_id: optionalID,
  conversation_id: optionalID,
  scope: z.enum(["personal", "project", "conversation"]).default("personal"),
  class: z.enum(["win", "lesson", "warning", "scar"]).optional(),
  outcome: z.enum(["good", "great", "bad", "worst"]).optional(),
  incident: text(4096), lesson: text(4096), source: text(1024),
  subject: text(256).optional(),
  features: labelsSchema.optional(), requires: labelsSchema.optional(), excludes: labelsSchema.optional(),
}).strict().superRefine((v, ctx) => {
  if (v.scope === "project" && !v.project_id) ctx.addIssue({ code: "custom", message: "Project scope requires project_id" });
  if (v.scope === "conversation" && !v.conversation_id) ctx.addIssue({ code: "custom", message: "Conversation scope requires conversation_id" });
});
export const feedbackSchema = z.object({
  operation_id: text(128), id: text(128), feedback_id: text(128), helpful: z.boolean(),
  project_id: optionalID, conversation_id: optionalID,
}).strict();
export const forgetSchema = z.object({ operation_id: text(128), id: text(128) }).strict();
export const toolSchemas = {
  profile_memory: profileSchema, init_memory: recallSchema, recall_memory: recallSchema,
  record_memory: recordSchema, feedback_memory: feedbackSchema, forget_memory: forgetSchema,
};
export type ToolName = keyof typeof toolSchemas;
export const envelopeSchema = z.object({
  version: z.literal(CONTRACT_VERSION),
  tool: z.enum(["profile_memory", "init_memory", "recall_memory", "record_memory", "feedback_memory", "forget_memory"]),
  arguments: z.record(z.string(), z.unknown()),
}).strict();
export type Envelope = z.infer<typeof envelopeSchema>;
export type Labels = Record<string, string[]>;
export interface Principal { tenant: string; user: string }
export interface OperationResponse {
  version: number; result: unknown; operation_id?: string; replayed?: boolean;
}
export interface Memory {
  id: string; tenant: string; owner: string; project: string; conversation: string;
  scope: "personal" | "project" | "conversation"; approved: boolean; retired: boolean;
  class: string; outcome: string; incident: string; lesson: string; source: string; subject: string;
  features: Labels; requires: Labels; excludes: Labels;
  created: string; updated: string; helpful: number; unhelpful: number;
}
export type RecallArgs = z.infer<typeof recallSchema>;
export function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") {
    const v = value as Record<string, unknown>;
    return `{${Object.keys(v).sort().map(k => `${JSON.stringify(k)}:${canonical(v[k])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}
