package activity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFreedesktopProviderCurrentState(t *testing.T) {
	p := &FreedesktopProvider{idleThreshold: 3 * time.Minute}

	cases := []struct {
		name     string
		locked   bool
		lockErr  error
		idleSecs uint32
		idleErr  error
		want     ActivityState
		wantErr  bool
	}{
		{
			name:     "locked ignores idle",
			locked:   true,
			idleSecs: 10 * 60,
			want:     ActivityLocked,
		},
		{
			name:     "active below threshold",
			locked:   false,
			idleSecs: 0,
			want:     ActivityActive,
		},
		{
			name:     "idle at threshold",
			locked:   false,
			idleSecs: 3 * 60,
			want:     ActivityIdle,
		},
		{
			name:     "lock error falls back to idle",
			locked:   false,
			lockErr:  errors.New("no lock service"),
			idleSecs: 0,
			want:     ActivityActive,
		},
		{
			name:     "idle error returns unknown",
			locked:   false,
			idleSecs: 0,
			idleErr:  errors.New("no idle service"),
			want:     ActivityUnknown,
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p.queryLocked = func(context.Context) (bool, error) { return tc.locked, tc.lockErr }
			p.queryIdleTime = func(context.Context) (uint32, error) { return tc.idleSecs, tc.idleErr }

			got, err := p.CurrentState(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("CurrentState() err = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("CurrentState() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFreedesktopProviderName(t *testing.T) {
	p := &FreedesktopProvider{name: "freedesktop"}
	if got := p.Name(); got != "freedesktop" {
		t.Fatalf("Name() = %q, want freedesktop", got)
	}

	p.name = "kde"
	if got := p.Name(); got != "kde" {
		t.Fatalf("Name() = %q, want kde", got)
	}
}
