# ultimate-kiro — build notes

Local Anthropic-compatible bridge that serves Kiro models to Claude Code
with maximum quality preservation.

## What this build does differently

1. **Max effort by default.** Thinking requests without an explicit effort
   run at `max` instead of a lower default, so Claude Code sessions always
   get full reasoning depth.
2. **Budget → effort mapping.** Claude Code expresses effort only as
   `thinking.budget_tokens`; this build maps the budget to the closest
   native tier (low / medium / high / xhigh / max), validated per model.
   Explicit `output_config.effort` still wins when present.
3. **`KIROCC_FORCE_EFFORT` env.** Pins a bridge instance to one tier
   (e.g. an eco instance with `KIROCC_FORCE_EFFORT=low`).
4. **Resolved effort logged at INFO** (`thinking budget mapped…`) so the
   active brain level is auditable per request.
5. **`KIROCC_HIDE_MODELS` env.** Comma-separated substrings hidden from the
   `/model` picker (for SKUs the account has no access to).
6. **Subscription-parity routing.** Unknown paths return Anthropic-shaped
   JSON 404s (feature-flag/org probes degrade gracefully); wrong-method
   hits return proper 405s.
7. **Context-window corrections.** GPT 5.6 family served as 1M (272k is
   only a pricing tier) with `[1m]` aliases so clients use full memory.
8. **MCP schema fidelity.** Tool schemas pass through nearly whole —
   verified live that only `$`-named keys break the backend. Local
   `$ref`/`$defs` dereferencing keeps factored MCP schemas complete.
9. **Windows releases** in `.goreleaser.yaml` alongside darwin/linux.

## Honest ceiling

No proxy is literally 100% — re-encoding costs: token budgets use tiktoken
cl100k_base (`internal/tokencount`), stop/max_tokens are enforced
adapter-side, plain thinking text is not replayed into history. Preserved: native effort
incl. budget mapping, native thinking/redacted streams, 1M routing,
tool calls incl. full schemas, images (base64), retries.

## Build / test

```powershell
$env:Path = [System.Environment]::GetEnvironmentVariable("Path","Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path","User")
go build -o ./bin/ultimate-kiro.exe ./cmd/kirocc
go test ./internal/models/... ./internal/app/... ./internal/reqconv/... ./internal/respconv/...
```
