// Package session manages relay sessions between agents and APK peers.
// V3: supports multiple agents per relay for AI-to-AI mesh communication.
package session

import (
	"sync"

	"github.com/KemonWoo/IMAgent/internal/p2p"
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
	Name string // display name (agents only)
	Role Role
	Send func(msg []byte) error
}

// AgentRef returns a p2p.AgentRef for gossip sync.
func (p *Peer) AgentRef() p2p.AgentRef {
	return p2p.AgentRef{ID: p.ID, Name: p.Name}
}

// Manager handles multi-agent sessions with one APK per relay.
type Manager struct {
	mu     sync.RWMutex
	agents map[string]*Peer // agentID → Peer (V3: multi-agent)
	apk    *Peer            // still single APK
	code   string           // current pairing code

	// Callbacks
	onAgentsChange func(agents []p2p.AgentRef)
}

// NewManager creates a session manager.
func NewManager() *Manager {
	return &Manager{
		agents: make(map[string]*Peer),
	}
}

// OnAgentsChange registers a callback invoked when local agents connect/disconnect.
func (m *Manager) OnAgentsChange(fn func(agents []p2p.AgentRef)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAgentsChange = fn
}

func (m *Manager) notifyAgentsChange() {
	if m.onAgentsChange != nil {
		refs := make([]p2p.AgentRef, 0, len(m.agents))
		for _, a := range m.agents {
			refs = append(refs, a.AgentRef())
		}
		m.onAgentsChange(refs)
	}
}

// RegisterAgent adds an agent peer (V3: multiple agents supported).
// Returns the agent's assigned ID.
func (m *Manager) RegisterAgent(peer *Peer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agents[peer.ID] = peer
	m.notifyAgentsChange()
}

// UnregisterAgent removes an agent by ID.
func (m *Manager) UnregisterAgent(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.agents, agentID)
	m.notifyAgentsChange()
}

// GetAgent returns an agent peer by ID, or nil.
func (m *Manager) GetAgent(agentID string) *Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[agentID]
}

// ListAgents returns all connected agent IDs.
func (m *Manager) ListAgents() []*Peer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Peer, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, a)
	}
	return out
}

// HasAgent checks if any Agent is connected.
func (m *Manager) HasAgent() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents) > 0
}

// AgentCount returns the number of connected agents.
func (m *Manager) AgentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// RegisterAPK sets the APK peer.
func (m *Manager) RegisterAPK(peer *Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = peer
	return true
}

// UnregisterAPK removes the APK peer.
func (m *Manager) UnregisterAPK() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = nil
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

// RouteToAgent delivers a message to a specific agent (used by P2P forwarding).
func (m *Manager) RouteToAgent(agentID string, msg []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	agent, ok := m.agents[agentID]
	if !ok {
		return nil // agent not connected
	}
	return agent.Send(msg)
}

// RouteFromAPK broadcasts a message from APK to all connected agents.
func (m *Manager) RouteFromAPK(msg []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, agent := range m.agents {
		agent.Send(msg)
	}
	return nil
}

// Reset clears the current APK pairing.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apk = nil
	m.code = ""
}

// AllAgentsRef returns AgentRefs for gossip.
func (m *Manager) AllAgentsRef() []p2p.AgentRef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	refs := make([]p2p.AgentRef, 0, len(m.agents))
	for _, a := range m.agents {
		refs = append(refs, a.AgentRef())
	}
	return refs
}
