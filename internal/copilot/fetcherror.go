package copilot

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrorKind classifies why a Copilot fetch failed so the UI can tell a
// transient upstream outage apart from a local configuration or authentication
// problem. The values are persisted with the snapshot and only translated at
// render time, so they must stay stable.
type ErrorKind string

const (
	// ErrorKindUnknown is used when the failure cannot be classified.
	ErrorKindUnknown ErrorKind = "unknown"
	// ErrorKindGHMissing means the configured gh binary could not be executed.
	ErrorKindGHMissing ErrorKind = "gh_missing"
	// ErrorKindAuth means gh has no (valid) GitHub credentials (HTTP 401).
	ErrorKindAuth ErrorKind = "auth"
	// ErrorKindForbidden means GitHub rejected the token's permissions (HTTP 403).
	ErrorKindForbidden ErrorKind = "forbidden"
	// ErrorKindNoSeat means the endpoint reported no Copilot access (HTTP 404).
	ErrorKindNoSeat ErrorKind = "no_seat"
	// ErrorKindRateLimited means the GitHub API rate limit was hit (HTTP 429).
	ErrorKindRateLimited ErrorKind = "rate_limited"
	// ErrorKindUnavailable means GitHub answered with a server error (HTTP 5xx).
	ErrorKindUnavailable ErrorKind = "unavailable"
	// ErrorKindNetwork means GitHub could not be reached at all.
	ErrorKindNetwork ErrorKind = "network"
	// ErrorKindParse means the response lacked the expected quota fields.
	ErrorKindParse ErrorKind = "parse"
)

// Transient reports whether a failure of this kind is expected to resolve on
// its own (upstream outage, throttling, connectivity), as opposed to a local
// problem the user has to fix.
func (k ErrorKind) Transient() bool {
	switch k {
	case ErrorKindUnavailable, ErrorKindRateLimited, ErrorKindNetwork:
		return true
	default:
		return false
	}
}

// kindMessages holds the stable English wording persisted in a snapshot's
// error message. The web UI renders the localized label from Kind instead.
var kindMessages = map[ErrorKind]string{
	ErrorKindGHMissing:   "GitHub CLI (gh) could not be executed",
	ErrorKindAuth:        "GitHub CLI is not authenticated",
	ErrorKindForbidden:   "GitHub denied access to the Copilot endpoint",
	ErrorKindNoSeat:      "no Copilot access for this account",
	ErrorKindRateLimited: "GitHub API rate limit exceeded",
	ErrorKindUnavailable: "GitHub is temporarily unavailable",
	ErrorKindNetwork:     "GitHub could not be reached",
	ErrorKindParse:       "unexpected response from GitHub",
	ErrorKindUnknown:     "Copilot fetch failed",
}

// FetchError is the error returned by a CreditProvider. It carries the
// classified Kind plus the verbatim upstream detail, so the fetcher can persist
// both and the UI can distinguish "GitHub is down" from "you are logged out".
type FetchError struct {
	// Kind is the classified failure category.
	Kind ErrorKind
	// Status is the HTTP status code, or 0 when the failure was not an HTTP error.
	Status int
	// Detail is the untranslated upstream message, trimmed and length-capped.
	Detail string
}

// Error implements error with a stable English message.
func (e *FetchError) Error() string {
	msg, ok := kindMessages[e.Kind]
	if !ok {
		msg = kindMessages[ErrorKindUnknown]
	}
	var b strings.Builder
	b.WriteString("copilot fetch: ")
	b.WriteString(msg)
	if e.Status > 0 {
		b.WriteString(" (HTTP ")
		b.WriteString(strconv.Itoa(e.Status))
		b.WriteString(")")
	}
	if e.Detail != "" {
		b.WriteString(": ")
		b.WriteString(e.Detail)
	}
	return b.String()
}

// KindOf returns the ErrorKind carried by err, ErrorKindUnknown for any other
// non-nil error, and an empty kind for nil.
func KindOf(err error) ErrorKind {
	if err == nil {
		return ""
	}
	var fe *FetchError
	if errors.As(err, &fe) {
		return fe.Kind
	}
	return ErrorKindUnknown
}

// httpStatusPattern matches the "(HTTP 503)" suffix gh appends to API errors.
var httpStatusPattern = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// authHints are gh messages emitted when no usable credentials are configured.
var authHints = []string{
	"gh auth login",
	"not logged in",
	"authentication token",
	"no oauth token",
	"bad credentials",
	"requires authentication",
}

// networkHints are transport-level failures surfaced by gh's HTTP client.
var networkHints = []string{
	"dial tcp",
	"no such host",
	"connection refused",
	"connection reset",
	"network is unreachable",
	"i/o timeout",
	"tls handshake",
	"proxyconnect",
}

// classifyGHError turns a failed `gh api` invocation into a FetchError by
// inspecting the process error and gh's stderr output.
func classifyGHError(runErr error, stderr string) *FetchError {
	fe := &FetchError{Kind: ErrorKindUnknown, Detail: cleanDetail(stderr)}

	switch {
	case errors.Is(runErr, exec.ErrNotFound), errors.Is(runErr, fs.ErrNotExist):
		fe.Kind = ErrorKindGHMissing
		if fe.Detail == "" && runErr != nil {
			fe.Detail = runErr.Error()
		}
		return fe
	case errors.Is(runErr, context.Canceled), errors.Is(runErr, context.DeadlineExceeded):
		fe.Kind = ErrorKindNetwork
		if fe.Detail == "" && runErr != nil {
			fe.Detail = runErr.Error()
		}
		return fe
	}

	lower := strings.ToLower(stderr)
	if m := httpStatusPattern.FindStringSubmatch(stderr); m != nil {
		fe.Status, _ = strconv.Atoi(m[1])
	}

	switch {
	case fe.Status == 401:
		fe.Kind = ErrorKindAuth
	case fe.Status == 403:
		if strings.Contains(lower, "rate limit") {
			fe.Kind = ErrorKindRateLimited
		} else {
			fe.Kind = ErrorKindForbidden
		}
	case fe.Status == 404:
		fe.Kind = ErrorKindNoSeat
	case fe.Status == 429:
		fe.Kind = ErrorKindRateLimited
	case fe.Status >= 500:
		fe.Kind = ErrorKindUnavailable
	case containsAny(lower, authHints...), exitCode(runErr) == 4:
		fe.Kind = ErrorKindAuth
	case containsAny(lower, networkHints...):
		fe.Kind = ErrorKindNetwork
	}

	if fe.Detail == "" && runErr != nil {
		fe.Detail = runErr.Error()
	}
	return fe
}

// exitCode returns the process exit code of err, or -1 when err is not an
// *exec.ExitError.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// detailMaxRunes caps how much upstream text is persisted with a snapshot.
const detailMaxRunes = 200

// cleanDetail normalizes gh's stderr into a single short line: the "gh: "
// prefix and the trailing "(HTTP nnn)" marker are dropped (the status is kept
// separately), HTML error pages are discarded, and the rest is length-capped.
func cleanDetail(stderr string) string {
	detail := strings.Join(strings.Fields(stderr), " ")
	detail = strings.TrimPrefix(detail, "gh: ")
	detail = strings.TrimSpace(httpStatusPattern.ReplaceAllString(detail, ""))
	if strings.HasPrefix(detail, "<") {
		// GitHub's HTML error page carries no useful message.
		return ""
	}
	if utf8.RuneCountInString(detail) > detailMaxRunes {
		detail = string([]rune(detail)[:detailMaxRunes]) + "…"
	}
	return detail
}
