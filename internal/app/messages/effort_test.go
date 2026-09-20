package messages

import (
	"context"
	"testing"

	"github.com/d-kuro/kirocc/internal/anthropic"
	"github.com/d-kuro/kirocc/internal/models"
)

func TestResolveEffort(t *testing.T) {
	tests := []struct {
		name         string
		kiroModel    string
		effort       string // request output_config.effort
		thinking     bool   // resolved thinking flag (type/[1m])
		want         string
		thinkingType string // request thinking.type ("" = absent)
		budget       int    // request thinking.budget_tokens (0 = absent)
	}{
		// Explicit effort passes through (validated against the model enum).
		{"explicit max on opus-5", "claude-opus-5", "max", false, "max", "", 0},
		{"explicit xhigh on opus-5", "claude-opus-5", "xhigh", true, "xhigh", "", 0},
		{"explicit max on opus-4.8", "claude-opus-4.8", "max", false, "max", "", 0},
		{"explicit xhigh on opus-4.8", "claude-opus-4.8", "xhigh", true, "xhigh", "", 0},
		{"explicit xhigh clamps on sonnet-4.6", "claude-sonnet-4.6", "xhigh", false, "max", "", 0},
		// sonnet-5 is the first Sonnet with a 5-value enum: xhigh is honored, not clamped.
		{"explicit xhigh honored on sonnet-5", "claude-sonnet-5", "xhigh", false, "xhigh", "", 0},
		{"explicit high on sonnet-5", "claude-sonnet-5", "high", false, "high", "", 0},
		// fable-5.1 shares the 5-value enum: xhigh is honored, not clamped.
		{"explicit xhigh honored on fable-5.1", "claude-fable-5.1", "xhigh", false, "xhigh", "", 0},
		{"explicit high on fable-5.1", "claude-fable-5.1", "high", false, "high", "", 0},

		// No effort + no thinking: nothing sent.
		{"no effort no thinking", "claude-opus-4.8", "", false, "", "", 0},

		// No effort but thinking enabled: fall back to the default effort so the
		// reasoning request still reaches the backend natively.
		// Ultimate-proxy tuning: default is max for highest quality.
		{"thinking only, effort-capable opus-5", "claude-opus-5", "", true, models.EffortMax, "", 0},
		{"thinking only, effort-capable opus-4.8", "claude-opus-4.8", "", true, models.EffortMax, "", 0},
		{"thinking only, effort-capable sonnet-4.6", "claude-sonnet-4.6", "", true, models.EffortMax, "", 0},

		// Thinking enabled but model has no effort schema: cannot express it, omit.
		{"thinking only, unsupported model", "claude-opus-4.5", "", true, "", "", 0},

		// Explicit effort wins even when thinking is also enabled.
		{"explicit low beats thinking default", "claude-opus-4.8", "low", true, "low", "", 0},

		// Invalid effort value is dropped; thinking default does NOT rescue an
		// explicitly-bad value (the explicit value was unrecognized, so we omit).
		{"invalid effort dropped despite thinking", "claude-opus-4.8", "bogus", true, "", "", 0},

		// Ultimate-proxy addition: thinking budget_tokens maps to the closest
		// native effort tier so Claude Code's configured depth survives the hop.
		{"budget 3000 maps to low", "claude-opus-5", "", true, "low", "enabled", 3000},
		{"budget 8000 maps to medium", "claude-opus-5", "", true, "medium", "enabled", 8000},
		{"budget 15000 maps to high", "claude-opus-5", "", true, "high", "enabled", 15000},
		{"budget 25000 maps to xhigh", "claude-opus-5", "", true, "xhigh", "enabled", 25000},
		{"budget 32000 maps to max", "claude-opus-5", "", true, "max", "enabled", 32000},
		{"budget xhigh clamps to max on 4-value sonnet-4.6", "claude-sonnet-4.6", "", true, "max", "enabled", 25000},
		{"explicit effort beats budget", "claude-opus-4.8", "low", true, "low", "enabled", 32000},

		// GPT 5.6 (reasoning style): enabled/absent omit the field so the
		// backend default (high) applies — never downgraded to medium.
		{"gpt absent thinking omits effort", "gpt-5.6-sol", "", false, "", "", 0},
		{"gpt enabled thinking omits effort (backend default high)", "gpt-5.6-sol", "", true, "", "", 0},
		// Explicit effort still honored.
		{"gpt explicit xhigh", "gpt-5.6-sol", "xhigh", false, "xhigh", "", 0},
		{"gpt explicit low with thinking", "gpt-5.6-terra", "low", true, "low", "", 0},
		// disabled → none, and it wins over explicit effort.
		{name: "gpt disabled maps to none", kiroModel: "gpt-5.6-sol", thinkingType: anthropic.ThinkingTypeDisabled, want: "none"},
		{name: "gpt disabled beats explicit effort", kiroModel: "gpt-5.6-luna", effort: "high", thinkingType: anthropic.ThinkingTypeDisabled, want: "none"},
		// Claude models never receive none: disabled just means no thinking fallback.
		{name: "claude disabled does not map to none", kiroModel: "claude-opus-4.8", thinkingType: anthropic.ThinkingTypeDisabled, want: ""},

		// Auto: backend selects the model, so effort is always dropped — even
		// when thinking is enabled (which would otherwise try a default effort).
		{name: "auto explicit effort dropped", kiroModel: "auto", effort: "high", want: ""},
		{name: "auto thinking only drops effort", kiroModel: "auto", thinking: true, want: ""},
		{name: "auto no intent drops effort", kiroModel: "auto", want: ""},
		{name: "auto claude-auto thinking drops effort", kiroModel: "claude-auto", thinking: true, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &anthropic.Request{}
			if tt.effort != "" {
				req.OutputConfig = &anthropic.OutputConfig{Effort: tt.effort}
			}
			if tt.thinkingType != "" || tt.budget > 0 {
				typ := tt.thinkingType
				if typ == "" {
					typ = anthropic.ThinkingTypeEnabled
				}
				req.Thinking = &anthropic.ThinkingConfig{Type: typ, BudgetTokens: tt.budget}
			}
			got := resolveEffort(context.Background(), tt.kiroModel, req, tt.thinking)
			if got != tt.want {
				t.Errorf("resolveEffort(%q, effort=%q, thinking=%v, budget=%d) = %q, want %q",
					tt.kiroModel, tt.effort, tt.thinking, tt.budget, got, tt.want)
			}
		})
	}
}

func TestResolveEffortForced(t *testing.T) {
	mkReq := func() *anthropic.Request {
		return &anthropic.Request{
			Thinking: &anthropic.ThinkingConfig{Type: anthropic.ThinkingTypeEnabled, BudgetTokens: 32000},
		}
	}
	t.Setenv("KIROCC_FORCE_EFFORT", "low")
	if got := resolveEffort(context.Background(), "claude-opus-5", mkReq(), true); got != "low" {
		t.Errorf("forced low = %q, want %q", got, "low")
	}
	// Invalid forced value falls back to budget mapping (32000 -> max).
	t.Setenv("KIROCC_FORCE_EFFORT", "bogus")
	if got := resolveEffort(context.Background(), "claude-opus-5", mkReq(), true); got != "max" {
		t.Errorf("invalid forced fallback = %q, want %q", got, "max")
	}
}
