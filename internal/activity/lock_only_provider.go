package activity

import (
	"context"
	"fmt"
)

// LockOnlyProvider reports pauses only from the screen lock state.
type LockOnlyProvider struct {
	logind      *logindChecker
	queryLocked func(context.Context) (bool, error)
}

// NewLockOnlyProvider returns a provider that reads only the logind lock hint.
func NewLockOnlyProvider() (*LockOnlyProvider, error) {
	logind, err := newLogindChecker()
	if err != nil {
		return nil, fmt.Errorf("logind lock state unavailable: %w", err)
	}
	p := &LockOnlyProvider{logind: logind}
	p.queryLocked = p.defaultLocked
	return p, nil
}

// Name returns the provider name.
func (p *LockOnlyProvider) Name() string { return "lock_only" }

// CurrentState returns locked when the screen is locked, active otherwise.
func (p *LockOnlyProvider) CurrentState(ctx context.Context) (ActivityState, error) {
	locked, err := p.queryLocked(ctx)
	if err != nil {
		return ActivityUnknown, fmt.Errorf("lock query: %w", err)
	}
	if locked {
		return ActivityLocked, nil
	}
	return ActivityActive, nil
}

func (p *LockOnlyProvider) defaultLocked(ctx context.Context) (bool, error) {
	return p.logind.isLocked(ctx)
}

// Close releases the logind connection.
func (p *LockOnlyProvider) Close() error {
	if p.logind != nil {
		return p.logind.Close()
	}
	return nil
}
