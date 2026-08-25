# Run 24 — #17 fixed and validated: zero-fail, coverage back to 100%, kiro zero-launch

Session cfbcbcb3, exit 0 — **705 pass / 0 fail / 1 uncertain, coverage 100%
(reached:true, gaps:0), ~784K/900K, 28m**. Zero kiro processes for the whole
run (shim-acp stub PATH on bridge-acp-real; the earlier launches were the
bridge repo's own test suite creating real kiro sessions — rule recorded).

## Fixes validated this run

- **open-agents #17** (bridge 205631b / parent cd27d14): ACP agent death now
  reports the exit_code status like the PTY path → the bridge stops the
  session → exit callback → task_error → retries → `workflow:task_failed`
  fires again. Proof: mission-seed PASS (its fail leg drove the previously
  gapped task_failed edge) and all four coverage gaps closed (100%).
- **cerberus repair target inheritance** (bc0c58a): WS repair replacements
  inherit the original's dial target when the emission's steps omit URLs
  (unit-locked; live confirmation rides future runs).

## Residuals

- mission-fanout's judge verdict degraded ("unable to determine") at the
  budget ceiling (784K/900K, last verdict) — execution PASSED; the judge
  starvation pattern recurs at each budget level. Next bump: 1M, or cap
  reflections.
- acpreal PASS again (real-LLM leg stable with the 300s window).
