# Tree-of-Thought Planning Configuration

`--deep-plan` enables Tree-of-Thought (ToT) beam search in the Scout head for
deeper test-strategy planning. The search has three tunable knobs, set in
`.cerberus/project.yaml`:

```yaml
settings:
  tot:
    beam_width: 3   # survivors kept after pruning each step
    generate_n: 5   # children expanded per surviving parent
    max_steps: 3    # propose→evaluate→select refinement rounds
```

All fields are optional; unset fields fall back to the defaults above
(`beam_width 3`, `generate_n 5`, `max_steps 3`), so omitting the block keeps the
prior hardcoded behavior.

## What each knob does

Beam search explores a strategy tree along three orthogonal dimensions:

| Field | Acts at | Constrains | Dimension |
|---|---|---|---|
| `generate_n` | propose | # children expanded per surviving parent | breadth |
| `beam_width` | select | # top candidates kept after pruning | survivors |
| `max_steps` | loop | # propose→evaluate→select refinement rounds | depth |

- **`generate_n`** — "how many you make." Each surviving parent spawns this many
  candidate strategies per step.
- **`beam_width`** — "how many you keep." After scoring, only the top-N
  candidates survive to the next step.
- **`max_steps`** — iterative refinement depth. Each round re-proposes from the
  survivors, so it is not a plain N-level tree expansion.

## Cost trade-offs

Per-step evaluate cost scales with `beam_width × generate_n`; total cost scales
roughly with `max_steps × (beam_width × generate_n)`.

- Raise **`max_steps`** for depth (linear cost).
- Raise **`generate_n`** for breadth (more candidates per parent).
- Raise **`beam_width`** to avoid pruning good strategies — the most expensive,
  because surviving candidates compound every subsequent step.

## Dual model tiering

Under Claude Code, ToT uses two model tiers automatically (no extra config):

- **propose** (strategy generation) runs on the SONNET tier — quality matters.
- **evaluate** (scoring) runs on the HAIKU tier — it is non-generative and
  high-frequency.

This is the same tier principle applied per-head in the main config; see
[Project Configuration — Automatic Model Tier Assignment](./project.md#automatic-model-tier-assignment).

## Reflexion memory

ToT mode also recalls cross-session memory (episodic + semantic) into the
propose prompt, controlled by the `settings.reflexion` block:

```yaml
settings:
  reflexion:
    episodic_limit: 10       # max L1 episodic entries recalled
    semantic_topk: 5         # max L2 semantic matches recalled
    semantic_threshold: 0.3  # min similarity for a semantic match
```

Defaults: `episodic_limit 10`, `semantic_topk 5`, `semantic_threshold 0.3`. The
evaluate step stays a pure scoring step and does not carry memory (it runs on
the cheap HAIKU tier).
