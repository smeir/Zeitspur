package copilot

import (
	"encoding/json"
	"strings"
	"time"
)

// rawQuota mirrors the JSON shape returned by `gh api copilot_internal/user`.
// Only the fields actually consumed are decoded; everything else is ignored.
type rawQuota struct {
	CopilotPlan           string             `json:"copilot_plan"`
	OrganizationLoginList []string           `json:"organization_login_list"`
	QuotaResetDateUTC     string             `json:"quota_reset_date_utc"`
	QuotaResetDate        string             `json:"quota_reset_date"`
	TokenBasedBilling     bool               `json:"token_based_billing"`
	QuotaSnapshots        map[string]rawSlot `json:"quota_snapshots"`
}

// rawSlot is a single quota_snapshot entry (e.g. premium_interactions).
type rawSlot struct {
	Entitlement       float64 `json:"entitlement"`
	QuotaRemaining    float64 `json:"quota_remaining"`
	Remaining         float64 `json:"remaining"`
	PercentRemaining  float64 `json:"percent_remaining"`
	TokenBasedBilling bool    `json:"token_based_billing"`
}

// Parse parses the stdout of `gh api copilot_internal/user` into a Snapshot
// with OK=true. Missing or malformed input yields nil.
func Parse(raw []byte) *Snapshot {
	var root rawQuota
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	slot, ok := root.QuotaSnapshots["premium_interactions"]
	if !ok {
		return nil
	}
	remaining := slot.QuotaRemaining
	if remaining == 0 {
		remaining = slot.Remaining
	}
	used := slot.Entitlement - remaining
	if used < 0 {
		used = 0
	}
	return &Snapshot{
		OK:                 true,
		Plan:               root.CopilotPlan,
		Organizations:      append([]string(nil), root.OrganizationLoginList...),
		EntitlementCredits: slot.Entitlement,
		RemainingCredits:   remaining,
		UsedCredits:        used,
		PercentRemaining:   slot.PercentRemaining,
		ResetAt:            parseResetDate(root.QuotaResetDateUTC, root.QuotaResetDate),
		TokenBasedBilling:  root.TokenBasedBilling || slot.TokenBasedBilling,
		WarningLevel:       WarningLevelFor(remaining, slot.Entitlement),
	}
}

// parseResetDate parses a quota reset timestamp. The API returns ISO 8601 with
// millisecond precision; fall back to RFC3339Nano and bare dates for robustness.
func parseResetDate(fields ...string) time.Time {
	for _, s := range fields {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Time{}
}
