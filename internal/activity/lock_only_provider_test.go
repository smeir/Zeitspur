package activity

import (
	"context"
	"errors"
	"testing"
)

func TestLockOnlyProviderCurrentState(t *testing.T) {
	cases := []struct {
		name    string
		locked  bool
		lockErr error
		want    ActivityState
		wantErr bool
	}{
		{
			name: "unlocked is active",
			want: ActivityActive,
		},
		{
			name:   "locked is locked",
			locked: true,
			want:   ActivityLocked,
		},
		{
			name:    "lock error returns unknown",
			lockErr: errors.New("no lock service"),
			want:    ActivityUnknown,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &LockOnlyProvider{
				queryLocked: func(context.Context) (bool, error) {
					return tc.locked, tc.lockErr
				},
			}

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
