package copilot

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestClassifyGHError_HTTPStatus(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		wantKind   ErrorKind
		wantStatus int
	}{
		{
			name:       "service outage",
			stderr:     "gh: No server is currently available to service your request. Sorry about that. (HTTP 503)\n",
			wantKind:   ErrorKindUnavailable,
			wantStatus: 503,
		},
		{
			name:       "bad gateway",
			stderr:     "gh: something broke (HTTP 502)\n",
			wantKind:   ErrorKindUnavailable,
			wantStatus: 502,
		},
		{
			name:       "expired token",
			stderr:     "gh: Bad credentials (HTTP 401)\n",
			wantKind:   ErrorKindAuth,
			wantStatus: 401,
		},
		{
			name:       "missing scope",
			stderr:     "gh: Resource protected by organization SAML enforcement (HTTP 403)\n",
			wantKind:   ErrorKindForbidden,
			wantStatus: 403,
		},
		{
			name:       "secondary rate limit",
			stderr:     "gh: You have exceeded a secondary rate limit (HTTP 403)\n",
			wantKind:   ErrorKindRateLimited,
			wantStatus: 403,
		},
		{
			name:       "rate limit",
			stderr:     "gh: API rate limit exceeded (HTTP 429)\n",
			wantKind:   ErrorKindRateLimited,
			wantStatus: 429,
		},
		{
			name:       "no seat",
			stderr:     "gh: Not Found (HTTP 404)\n",
			wantKind:   ErrorKindNoSeat,
			wantStatus: 404,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fe := classifyGHError(errors.New("exit status 1"), tc.stderr)
			if fe.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", fe.Kind, tc.wantKind)
			}
			if fe.Status != tc.wantStatus {
				t.Errorf("Status = %d, want %d", fe.Status, tc.wantStatus)
			}
			if strings.Contains(fe.Detail, "(HTTP") {
				t.Errorf("Detail should not repeat the status: %q", fe.Detail)
			}
		})
	}
}

func TestClassifyGHError_NoStatus(t *testing.T) {
	tests := []struct {
		name     string
		runErr   error
		stderr   string
		wantKind ErrorKind
	}{
		{
			name:     "not logged in",
			runErr:   errors.New("exit status 4"),
			stderr:   "To get started with GitHub CLI, please run: gh auth login\n",
			wantKind: ErrorKindAuth,
		},
		{
			name:     "dns failure",
			runErr:   errors.New("exit status 1"),
			stderr:   "error connecting to api.github.com: dial tcp: lookup api.github.com: no such host\n",
			wantKind: ErrorKindNetwork,
		},
		{
			name:     "binary missing",
			runErr:   exec.ErrNotFound,
			stderr:   "",
			wantKind: ErrorKindGHMissing,
		},
		{
			name:     "context cancelled",
			runErr:   context.Canceled,
			stderr:   "",
			wantKind: ErrorKindNetwork,
		},
		{
			name:     "unclassified",
			runErr:   errors.New("exit status 1"),
			stderr:   "something odd happened\n",
			wantKind: ErrorKindUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fe := classifyGHError(tc.runErr, tc.stderr)
			if fe.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", fe.Kind, tc.wantKind)
			}
			if fe.Detail == "" {
				t.Error("Detail should fall back to the process error")
			}
		})
	}
}

func TestCleanDetail(t *testing.T) {
	if got := cleanDetail("gh: Bad credentials (HTTP 401)\n"); got != "Bad credentials" {
		t.Errorf("cleanDetail = %q, want %q", got, "Bad credentials")
	}
	if got := cleanDetail("<!DOCTYPE html>\n<html>...</html>"); got != "" {
		t.Errorf("HTML error page should be dropped, got %q", got)
	}
	long := "gh: " + strings.Repeat("x", detailMaxRunes+50)
	got := cleanDetail(long)
	if len([]rune(got)) != detailMaxRunes+1 {
		t.Errorf("cleanDetail length = %d runes, want %d plus ellipsis", len([]rune(got)), detailMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated detail should end with an ellipsis, got %q", got)
	}
}

func TestErrorKindTransient(t *testing.T) {
	transient := []ErrorKind{ErrorKindUnavailable, ErrorKindRateLimited, ErrorKindNetwork}
	for _, k := range transient {
		if !k.Transient() {
			t.Errorf("%q should be transient", k)
		}
	}
	permanent := []ErrorKind{ErrorKindAuth, ErrorKindForbidden, ErrorKindNoSeat, ErrorKindGHMissing, ErrorKindParse, ErrorKindUnknown}
	for _, k := range permanent {
		if k.Transient() {
			t.Errorf("%q should not be transient", k)
		}
	}
}

func TestFetchErrorMessage(t *testing.T) {
	fe := &FetchError{Kind: ErrorKindUnavailable, Status: 503, Detail: "No server is currently available"}
	got := fe.Error()
	for _, want := range []string{"GitHub is temporarily unavailable", "HTTP 503", "No server is currently available"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, want it to contain %q", got, want)
		}
	}
}

func TestKindOf(t *testing.T) {
	if got := KindOf(nil); got != "" {
		t.Errorf("KindOf(nil) = %q, want empty", got)
	}
	if got := KindOf(errors.New("boom")); got != ErrorKindUnknown {
		t.Errorf("KindOf(plain) = %q, want %q", got, ErrorKindUnknown)
	}
	wrapped := errors.Join(errors.New("context"), &FetchError{Kind: ErrorKindAuth})
	if got := KindOf(wrapped); got != ErrorKindAuth {
		t.Errorf("KindOf(wrapped) = %q, want %q", got, ErrorKindAuth)
	}
}
