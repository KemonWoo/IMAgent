// Package session manages a single-session relay between one Agent and one APK.
package session

import (
	"sync"
)

// Role identifies the peer.
type Role string

const (
	RoleAgent Role = "agent"
	RoleAPK   Role = "apk"
)

// Peer represents a connected client.
type Peer struct {
	ID   string
	Role Role
	Send func(msg []byte) error
}

// Manager handles the single-session topology (V1: 1 Agent + 1 APK).
type Manager struct {
	mu    sync.RWMutex
	agent *Peer
	apk   *Peer
	code  string // current pairing code
}

// NewManager creates a session manager.
func NewManager() *Manager {
	return &Manager{}
}

// RegisterAgent sets the Agent peer.
func (m *Manager) RegisterAgent(peer *Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agent = peer
}

// RegisterAPK sets the APK peer.
func (m *Manager) RegisterAPK(peer *Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = peer
	return true
}

// SetCode stores the current pairing code.
func (m *Manager) SetCode(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.code = code
}

// GetCode returns the current pairing code.
func (m *Manager) GetCode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.code
}

// VerifyCode checks if the APK's code matches.
func (m *Manager) VerifyCode(code string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.code != "" && m.code == code
}

// HasAgent checks if an Agent is connected.
func (m *Manager) HasAgent() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agent != nil
}

// HasAPK checks if an APK is connected.
func (m *Manager) HasAPK() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.apk != nil
}

// RouteFromAgent sends a message to the APK.
func (m *Manager) RouteFromAgent(msg []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.apk == nil {
		return nil
	}
	return m.apk.Send(msg)
}

// RouteFromAPK sends a message to the Agent.
func (m *Manager) RouteFromAPK(msg []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.agent == nil {
		return nil
	}
	return m.agent.Send(msg)
}

// Reset clears the current pairing.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = nil
	m.code = ""
}

// UnregisterAPK removes the APK peer.
func (m *Manager) UnregisterAPK() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = nil
}
