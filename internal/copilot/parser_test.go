package copilot

import (
	"encoding/json"
	"testing"
)

func TestParseFullResponse(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"copilot_plan":            "business",
		"organization_login_list": []string{"bosch-copilot"},
		"quota_reset_date_utc":    "2026-07-01T00:00:00.000Z",
		"token_based_billing":     true,
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":         3000,
				"quota_remaining":     1197.7,
				"percent_remaining":   39.9,
				"token_based_billing": true,
			},
		},
	})
	snap := Parse(raw)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.Plan != "business" {
		t.Errorf("plan = %q, want business", snap.Plan)
	}
	if len(snap.Organizations) != 1 || snap.Organizations[0] != "bosch-copilot" {
		t.Errorf("organizations = %v, want [bosch-copilot]", snap.Organizations)
	}
	if snap.EntitlementCredits != 3000 {
		t.Errorf("entitlement = %v, want 3000", snap.EntitlementCredits)
	}
	if snap.RemainingCredits != 1197.7 {
		t.Errorf("remaining = %v, want 1197.7", snap.RemainingCredits)
	}
	if snap.UsedCredits != 1802.3 {
		t.Errorf("used = %v, want 1802.3", snap.UsedCredits)
	}
	if snap.PercentRemaining != 39.9 {
		t.Errorf("percent = %v, want 39.9", snap.PercentRemaining)
	}
	if snap.ResetAt.Year() != 2026 || snap.ResetAt.Month() != 7 || snap.ResetAt.Day() != 1 {
		t.Errorf("reset = %v, want 2026-07-01", snap.ResetAt)
	}
	if !snap.TokenBasedBilling {
		t.Errorf("token_based_billing = false, want true")
	}
	if snap.WarningLevel != WarningNone {
		t.Errorf("warning = %q, want none", snap.WarningLevel)
	}
}

func TestParseFallsBackToRemainingField(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement": 3000,
				"remaining":   200,
			},
		},
	})
	snap := Parse(raw)
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.RemainingCredits != 200 {
		t.Errorf("remaining = %v, want 200", snap.RemainingCredits)
	}
	if snap.UsedCredits != 2800 {
		t.Errorf("used = %v, want 2800", snap.UsedCredits)
	}
	if snap.WarningLevel != WarningWarning {
		t.Errorf("warning = %q, want warning", snap.WarningLevel)
	}
}

func TestParseCriticalWhenExhausted(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":     3000,
				"quota_remaining": 0,
			},
		},
	})
	snap := Parse(raw)
	if snap.WarningLevel != WarningCritical {
		t.Errorf("warning = %q, want critical", snap.WarningLevel)
	}
	if snap.UsedCredits != 3000 {
		t.Errorf("used = %v, want 3000", snap.UsedCredits)
	}
}

func TestParseNoticeAt75Percent(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":     1000,
				"quota_remaining": 250,
			},
		},
	})
	snap := Parse(raw)
	if snap.WarningLevel != WarningNotice {
		t.Errorf("warning = %q, want notice", snap.WarningLevel)
	}
}

func TestParseRejectsMalformed(t *testing.T) {
	if Parse([]byte("{")) != nil {
		t.Error("expected nil for malformed JSON")
	}
}

func TestParseRejectsMissingPremiumInteractions(t *testing.T) {
	raw := mustJSON(t, map[string]any{
		"copilot_plan":    "business",
		"quota_snapshots": map[string]any{},
	})
	if Parse(raw) != nil {
		t.Error("expected nil when premium_interactions missing")
	}
}

func TestParseClampsResetNegative(t *testing.T) {
	// If remaining exceeds entitlement (shouldn't happen, but be defensive),
	// used must not be negative.
	raw := mustJSON(t, map[string]any{
		"quota_snapshots": map[string]any{
			"premium_interactions": map[string]any{
				"entitlement":     100,
				"quota_remaining": 500,
			},
		},
	})
	snap := Parse(raw)
	if snap.UsedCredits != 0 {
		t.Errorf("used = %v, want 0 (clamped)", snap.UsedCredits)
	}
}

func TestWarningLevelFor(t *testing.T) {
	cases := []struct {
		remaining, entitlement float64
		want                   WarningLevel
	}{
		{750, 1000, WarningNone},
		{250, 1000, WarningNotice},
		{100, 1000, WarningWarning},
		{0, 1000, WarningCritical},
		{0, 0, WarningNone},
	}
	for _, c := range cases {
		if got := WarningLevelFor(c.remaining, c.entitlement); got != c.want {
			t.Errorf("WarningLevelFor(%v, %v) = %q, want %q", c.remaining, c.entitlement, got, c.want)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
