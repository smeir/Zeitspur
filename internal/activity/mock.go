package activity

import "context"

// MockProvider is a programmable provider for tests.
type MockProvider struct {
	name  string
	state ActivityState
	err   error
}

// NewMockProvider returns a mock provider with the given initial state.
func NewMockProvider(state ActivityState) *MockProvider {
	return &MockProvider{name: "mock", state: state}
}

// Name returns the provider name.
func (m *MockProvider) Name() string { return m.name }

// CurrentState returns the configured state.
func (m *MockProvider) CurrentState(ctx context.Context) (ActivityState, error) {
	return m.state, m.err
}

// SetState updates the state returned by CurrentState.
func (m *MockProvider) SetState(state ActivityState) {
	m.state = state
}

// SetError updates the error returned by CurrentState.
func (m *MockProvider) SetError(err error) {
	m.err = err
}

// Close is a no-op; the mock holds no resources.
func (m *MockProvider) Close() error { return nil }
