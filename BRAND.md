# 🐘 Elephant

**Persistent experience for coding agents.**

**Agents forget. Elephants don’t.**

Elephant is an open-source experience layer for coding agents. It stores useful lessons from earlier work. It reads the current environment and recalls relevant Memories. A Recall Budget limits the returned text.

An agent completes work. The result becomes an Experience. The agent extracts a useful Memory and its Signals. Elephant stores it through Imprint. On a later task, a Fingerprint helps Recall find a relevant Memory.

The goal is useful Recall. Elephant does not need to load whole conversations into the context window.

## Product terms

| Term | Meaning | Current support |
| --- | --- | --- |
| Elephant | The complete experience layer. | Local engine, CLI, MCP and UI. |
| Experience | Evidence from an observed event. | Incident text and source on a Memory; no separate event table yet. |
| Memory | A durable lesson from an Experience. | Stored in the journal. |
| Win | An approach that worked well. | Memory Class. |
| Lesson | Useful knowledge with no strong outcome. | Memory Class. |
| Warning | A risk under stated conditions. | Memory Class. |
| Scar | Knowledge from a serious failure. | Memory Class; no untested ranking boost. |
| Signal | A fact about where a Memory can apply. | Literal features and authored requirements/exclusions. |
| Fingerprint | Known facts about the current environment. | Bounded manifest reads and explicit settings. |
| Recall | Select useful Memories for the current task. | Lexical matching, context matching and ranking. |
| Recall Budget | The limit on returned experience text. | UTF-8 byte budget, plus a Memory count limit. |
| Imprint | Store a Memory with evidence. | Explicit record call; no automatic transcript extraction. |
| Reinforcement | Independent evidence supports a lesson again. | Observed feedback exists; independent Experience grouping is future work. |
| Contradiction | Evidence supports different advice. | Same-subject alternatives are flagged as possible; contradiction is not proved. |
| Decay | Old or weak knowledge has less ranking weight. | A simple age factor; no class-specific decay yet. |
| Herd | Agents or projects that share approved knowledge. | Private reuse and reviewed local team export/import. |
| Herd Memory | Useful knowledge shared across projects. | Local scope rules; no authenticated cloud sync. |
| Trail | A link between stored facts. | Derived origin, Signal and evidence links. |
| Memory Map | A view of connected Memories and evidence. | Interactive local view and JSON CLI/API graph. |
| Memory Palace | The human-facing Elephant interface. | Local dashboard, library, Map and Recall activity. |
| Recall Path | Evidence that explains a Recall result. | Match metadata through `why`; no causal confidence claim. |
| Save | A used Memory helps with an observed task. | Helpful feedback is supplied evidence; prevention/time saved is not inferred. |
| Unanchored Experience | An Experience with no known project. | `--unattached` and optional conversation ID. |
| Agent Adapter | A host-specific connection to Elephant. | Portable CLI/MCP and documented client settings. |

Use these terms in the product. Keep internal types clear: Memory, Signal, Fingerprint, Trail, RecallResult and LanguagePolicy. Use no hidden elephant-themed database names.

## Commands

```sh
elephant init --task 'Bound query concurrency'
elephant status
elephant remember --file lesson.json
elephant imprint --file lesson.json
elephant recall --task 'Bound query concurrency'
elephant why --task 'Bound query concurrency' --id MEMORY_ID
elephant inspect --id MEMORY_ID
elephant scars
elephant map
elephant fingerprint
elephant stats
elephant palace
elephant forget --id MEMORY_ID
```

`remember` and `imprint` store an authored Memory. They do not scan recent conversations. `why` runs Recall for the given task and returns its match facts. It does not invent a reason for an omitted Memory. Map links come from stored facts. Signal overlap does not attach an unanchored Experience to a project.

## Data compatibility

The default new journal is `~/.elephant/events.jsonl`. If only the old `~/.agent-memory/events.jsonl` exists, reuse it. Read `.elephant.json` first. Read `.agent-memory.json` only when the new project file is absent.

Keep legacy commands `profile`, `record`, `list` and `ui` as aliases. Keep the six MCP tool names and existing JSON field names so clients and journals continue to work.

Old outcomes map to Classes: `great` → Win, `good` → Lesson, `bad` → Warning, `worst` → Scar. A caller can supply a Class on a new Memory. Existing records stay unchanged.

Current private scopes are personal, project and conversation. Team scope requires review. Session, repository, project, Herd and global are useful future concepts. They are not new authorization rules in this release.

## Recall and metrics

The retrieval math remains documented in DESIGN.md. A Scar still needs relevant task evidence and valid requirements. A severe label cannot bypass a scope rule or a Recall Budget.

Do not show invented confidence, saves or time reductions. The current token baseline loads all eligible summaries. It does not represent the full historical conversations. Future historical-compression metrics need source lengths and tokenizer evidence.
