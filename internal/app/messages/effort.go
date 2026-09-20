package messages

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/d-kuro/kirocc/internal/anthropic"
	"github.com/d-kuro/kirocc/internal/logging"
	"github.com/d-kuro/kirocc/internal/models"
)

// formatContextWindow renders a context window size as a human label like
// "200k" or "1M" for log output.
func formatContextWindow(size int) string {
	if size >= 1_000_000 {
		return fmt.Sprintf("%dM", size/1_000_000)
	}
	return fmt.Sprintf("%dk", size/1000)
}

// defaultThinkingEffort is the native effort level sent when a request enables
// reasoning (thinking.type or the [1m] suffix) without an explicit
// output_config.effort. kiro-cli 2.10.0 expresses all reasoning depth
// through effort, so this carries the "thinking on" intent to the backend.
// Tuned to max for highest answer quality (ultimate-proxy build): Claude Code
// expresses effort only via thinking budget, which kirocc otherwise maps to
// medium, so without this every Claude Code thinking request would run below
// the user's configured effort.
const defaultThinkingEffort = models.EffortMax

// resolveEffort maps the request's reasoning intent to a native effort level the
// resolved Kiro model accepts.
//
// Claude (output_config style) precedence:
//
//  1. An explicit, recognized output_config.effort wins (validated/clamped to
//     the model's enum; xhigh on a 4-value model clamps to max).
//  2. Otherwise, if reasoning is enabled via thinking/[1m], fall back to
//     defaultThinkingEffort so the intent still reaches the backend natively.
//  3. Otherwise (and for unrecognized or unsupported effort), return "" so
//     additionalModelRequestFields is omitted.
//
// GPT 5.6 (reasoning style) differs deliberately:
//
//   - thinking.type disabled → "none" (explicit opt-out, wins over explicit effort)
//   - explicit output_config.effort → validated as usual
//   - thinking enabled/absent → "" (field omitted; the backend default is high,
//     matching kiro-cli, which never sends the field)
//
// An explicit but unrecognized effort is dropped without invoking the thinking
// fallback — the client asked for something specific that we couldn't honor, so
// we don't silently substitute a guess.
// effortForBudget maps an Anthropic thinking budget_tokens value to the
// closest native Kiro effort tier. Ultimate-proxy addition: upstream kirocc
// parses budget_tokens but never forwards it, so Claude Code's configured
// effort (which travels only as a budget number) was silently lost and every
// request fell back to the default. Explicit output_config.effort still wins
// over this mapping.
func effortForBudget(budget int) string {
	switch {
	case budget < 4000:
		return models.EffortLow
	case budget < 10000:
		return models.EffortMedium
	case budget < 20000:
		return models.EffortHigh
	case budget < 32000:
		return models.EffortXHigh
	default:
		return models.EffortMax
	}
}

func resolveEffort(ctx context.Context, kiroModel string, req *anthropic.Request, thinking bool) string {
	_, short := logging.TraceIDs(ctx)
	requested := req.Effort()
	reasoningStyle := models.IsReasoningModel(kiroModel)

	// Ultimate-proxy addition: KIROCC_FORCE_EFFORT pins every request to one
	// native tier (e.g. an eco bridge with KIROCC_FORCE_EFFORT=low). Wins over
	// explicit effort, budget mapping and defaults; validated/clamped against
	// the model's enum, invalid values fall through with a warning.
	if forced := strings.ToLower(strings.TrimSpace(os.Getenv("KIROCC_FORCE_EFFORT"))); forced != "" {
		if resolved := models.ResolveEffort(kiroModel, forced); resolved != "" {
			slog.InfoContext(ctx, "effort forced via KIROCC_FORCE_EFFORT",
				"trace_id", short, "model", kiroModel, "effort", resolved)
			return resolved
		}
		slog.WarnContext(ctx, "KIROCC_FORCE_EFFORT not supported by model, ignoring",
			"trace_id", short, "model", kiroModel, "forced_effort", forced)
	}

	if reasoningStyle && req.IsThinkingDisabled() {
		return models.ResolveEffort(kiroModel, models.EffortNone)
	}

	if requested != "" {
		resolved := models.ResolveEffort(kiroModel, requested)
		if resolved != requested {
			switch resolved {
			case "":
				slog.WarnContext(ctx, "effort not honored, dropping",
					"trace_id", short, "model", kiroModel, "requested_effort", requested)
			default:
				slog.WarnContext(ctx, "requested effort not supported by model, downgrading",
					"trace_id", short, "model", kiroModel, "requested_effort", requested, "effort", resolved)
			}
		}
		return resolved
	}

	// Ultimate-proxy addition: a thinking budget maps to the closest native
	// effort tier so the client's configured depth survives the hop.
	// Validated/clamped against the model's enum like explicit effort.
	if req.Thinking != nil && req.Thinking.BudgetTokens > 0 {
		mapped := effortForBudget(req.Thinking.BudgetTokens)
		if resolved := models.ResolveEffort(kiroModel, mapped); resolved != "" {
			slog.InfoContext(ctx, "thinking budget mapped to native effort",
				"trace_id", short, "model", kiroModel,
				"budget_tokens", req.Thinking.BudgetTokens, "effort", resolved)
			return resolved
		}
	}

	// Reasoning-style models omit the field for enabled/absent: the backend
	// default (high) already exceeds defaultThinkingEffort.
	if reasoningStyle {
		return ""
	}

	if thinking {
		resolved := models.ResolveEffort(kiroModel, defaultThinkingEffort)
		if resolved != "" {
			slog.InfoContext(ctx, "thinking enabled without effort, using default effort",
				"trace_id", short, "model", kiroModel, "effort", resolved)
		}
		return resolved
	}

	return ""
}
