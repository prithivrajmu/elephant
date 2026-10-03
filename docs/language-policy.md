# Elephant writing policy

Use ASD-STE100 as the only writing guide for Elephant Memory summaries. The default target is 80%. A user can set a target from 80% to 100%.

```sh
elephant language
elephant language --ste-target 100
elephant language --text 'Bound active queries to the pool limit.'
elephant setup --wizard
```

Memory Palace has a **STE** control. The target is saved beside the selected store in `<store>.settings.json` (for example, `memories.sqlite.settings.json`). It applies to new summaries from CLI, MCP, UI and import. Restart the agent host after a change to refresh its startup instructions. Record checks read the current target on each call.

## What the target means

The target is a product setting. It is not a measured percentage of standard compliance.

| Target | New Memory summaries |
| --- | --- |
| 80–99% | Store the summary. Return local writing warnings. |
| 100% | Reject the summary if a local check fails. Return a reason. |

Intermediate values change the stated writing target in agent instructions. They use the same warning behavior as 80%. They do not compute a proportional compliance score.

Local checks cover three areas: summary sentences of 25 words or less, selected complex phrases, and selected contractions. For example, a check suggests `use` instead of `utilize`. Code spans must have paired backticks. The word count is approximate. It is not the standard's complete counting method.

Agent instructions also request short sentences, active verbs, one instruction per sentence, and one term per concept. Procedural text should use 20 words or less. The built-in checker does not classify procedural text or validate this 20-word limit.

The checker does not include the full official dictionary. It does not check every approved meaning, part of speech, technical term or writing rule. Passing the local checks is **not** proof of full ASD-STE100 compliance. There is no official “80% mode.” ASD and STEMG do not certify software checkers. Full review needs the official standard and a qualified writer.

## Text boundaries

- Check the reusable Memory summary (`lesson`). Keep the original Experience (`incident`) as evidence.
- Keep source references, identifiers, code, commands, paths and Signals exact.
- Preserve existing Memories and their history. Changing the target does not rewrite them.
- Use the Elephant vocabulary as the product glossary. Treat product names and protocol identifiers as technical terms.
- Keep the supplied brand slogan exact: “Agents forget. Elephants don’t.” It is brand text, not a validated technical instruction.

Elephant does not call a model to rewrite text. It does not translate raw conversations. The agent authors a summary under the writing guide. Local checks then give warnings or reject it. Stored writing reports show the target and checks used at Imprint time.

## Full-standard route

Add an independently configured dictionary/rule checker and a reviewed technical glossary before claiming full validation. Preserve the original text, store review evidence, and identify the checker version. Do not silently change meaning to pass a check.

Official sources, consulted 2026-10-03:

- Standard overview and permitted technical terms: https://www.asd-ste100.org/about_STE.html
- Checker limits and writer responsibility: https://asd-ste100.org/STEsoftware.html
- Official Issue 9 request: https://www.asd-ste100.org/

Elephant is not endorsed by ASD or STEMG. It does not redistribute the standard or its dictionary.

