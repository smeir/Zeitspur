package copilot

import (
	"bytes"
	"context"
	"os"
	"os/exec"
)

// CreditProvider fetches a single Copilot credit snapshot. It mirrors the
// ActivityProvider abstraction so the fetcher stays testable.
type CreditProvider interface {
	// Fetch returns a single quota snapshot, or an error describing why the
	// fetch failed (the fetcher records an OK=false row in that case).
	Fetch(ctx context.Context) (*Snapshot, error)
	// Close releases any resources held by the provider.
	Close() error
}

// GHCLIProvider fetches credits by shelling out to `gh api copilot_internal/user`.
// It relies on the user's existing `gh auth login` credentials, so no token
// management is needed in Zeitspur itself.
type GHCLIProvider struct {
	ghPath string
	env    []string
}

// NewGHCLIProvider returns a provider that invokes the given gh binary.
func NewGHCLIProvider(ghPath string) *GHCLIProvider {
	return &GHCLIProvider{ghPath: ghPath, env: nonInteractiveEnv()}
}

// Fetch implements CreditProvider.
func (p *GHCLIProvider) Fetch(ctx context.Context) (*Snapshot, error) {
	cmd := exec.CommandContext(ctx, p.ghPath, "api", "copilot_internal/user")
	// Disable any interactive prompts so a missing auth fails fast instead of
	// hanging the daemon. Mirrors Tauwerk's NON_INTERACTIVE_GH_ENV.
	cmd.Env = p.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, classifyGHError(err, stderr.String())
	}
	snap := Parse(stdout.Bytes())
	if snap == nil {
		return nil, &FetchError{
			Kind:   ErrorKindParse,
			Detail: "no premium_interactions quota in response",
		}
	}
	return snap, nil
}

// Close implements CreditProvider. The gh provider holds no resources.
func (p *GHCLIProvider) Close() error { return nil }

// nonInteractiveEnv returns the current environment with gh/git prompting
// disabled so a missing token fails fast instead of blocking the daemon. The
// existing environment (PATH, HOME, GH config) is preserved.
func nonInteractiveEnv() []string {
	env := append([]string(nil), os.Environ()...)
	overrides := map[string]string{
		"GH_PROMPT_DISABLED":  "1",
		"GCM_INTERACTIVE":     "never",
		"GIT_TERMINAL_PROMPT": "0",
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}

// MockProvider is a test CreditProvider that returns a queued snapshot.
type MockProvider struct {
	Snapshot *Snapshot
	Err      error
}

// NewMockProvider returns a MockProvider that returns the given snapshot.
func NewMockProvider(snap *Snapshot) *MockProvider { return &MockProvider{Snapshot: snap} }

// Fetch implements CreditProvider.
func (m *MockProvider) Fetch(ctx context.Context) (*Snapshot, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Snapshot, nil
}

// Close implements CreditProvider.
func (m *MockProvider) Close() error { return nil }
