// Package activity detects and records user activity.
package activity

import "context"

// ActivityState represents the current high-level user state.
type ActivityState string

const (
	ActivityActive    ActivityState = "active"
	ActivityIdle      ActivityState = "idle"
	ActivityLocked    ActivityState = "locked"
	ActivitySuspended ActivityState = "suspended"
	ActivityUnknown   ActivityState = "unknown"
)

// ActivityProvider is the abstraction for different activity sources.
type ActivityProvider interface {
	Name() string
	CurrentState(ctx context.Context) (ActivityState, error)
}

// EventType values stored in the activity_events table.
type EventType string

const (
	EventActive         EventType = "active"
	EventIdle           EventType = "idle"
	EventLocked         EventType = "locked"
	EventUnlocked       EventType = "unlocked"
	EventSuspend        EventType = "suspend"
	EventResume         EventType = "resume"
	EventProviderError  EventType = "provider_error"
	EventServiceStarted EventType = "service_started"
	EventServiceStopped EventType = "service_stopped"
)
