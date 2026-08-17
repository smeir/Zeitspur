// Package copilot periodically fetches GitHub Copilot AI credit quota via the
// GitHub CLI and stores the snapshots in SQLite. Only high-level quota numbers
// (entitlement, remaining, used, percent, reset date) are captured — never
// prompts, models, or content — so the project's privacy model stays intact.
package copilot

import "time"

// WarningLevel classifies how close the Copilot credit quota is to exhaustion.
type WarningLevel string

const (
	// WarningNone means usage is below 75% of the entitlement.
	WarningNone WarningLevel = "none"
	// WarningNotice means at least 75% of the entitlement has been used.
	WarningNotice WarningLevel = "notice"
	// WarningWarning means at least 90% of the entitlement has been used.
	WarningWarning WarningLevel = "warning"
	// WarningCritical means the entitlement is fully consumed.
	WarningCritical WarningLevel = "critical"
)

// Snapshot is a single point-in-time observation of the Copilot credit quota.
// It mirrors the data returned by `gh api copilot_internal/user` (the
// quota_snapshots.premium_interactions branch), plus a fetch status.
type Snapshot struct {
	// FetchedAt is when the snapshot was collected, in UTC.
	FetchedAt time.Time
	// OK is true when the fetch succeeded and the quota fields are populated.
	OK bool
	// Plan is the Copilot plan, e.g. "business" or "individual".
	Plan string
	// Organizations is the list of organization logins the seat belongs to.
	Organizations []string
	// EntitlementCredits is the total credits for the billing period.
	EntitlementCredits float64
	// RemainingCredits is the credits still available.
	RemainingCredits float64
	// UsedCredits is EntitlementCredits - RemainingCredits.
	UsedCredits float64
	// PercentRemaining is the percentage of credits left (0-100).
	PercentRemaining float64
	// ResetAt is when the billing period resets and credits replenish.
	ResetAt time.Time
	// TokenBasedBilling is true when the plan bills per token.
	TokenBasedBilling bool
	// WarningLevel is the derived severity (none/notice/warning/critical).
	WarningLevel WarningLevel
	// ErrorMessage is set when OK is false, describing the fetch failure in
	// stable English (including the upstream detail).
	ErrorMessage string
	// ErrorKind is set when OK is false and classifies the failure so the UI
	// can distinguish a transient GitHub outage from a local auth problem.
	ErrorKind ErrorKind
}

// WarningLevelFor returns the warning level for the given usage ratio.
func WarningLevelFor(remaining, entitlement float64) WarningLevel {
	if entitlement <= 0 {
		return WarningNone
	}
	used := entitlement - remaining
	if used < 0 {
		used = 0
	}
	ratio := used / entitlement
	switch {
	case ratio >= 1:
		return WarningCritical
	case ratio >= 0.9:
		return WarningWarning
	case ratio >= 0.75:
		return WarningNotice
	default:
		return WarningNone
	}
}
