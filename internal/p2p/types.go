// Package p2p implements peer-to-peer mesh networking for IMAgent Relay.
// Supports node discovery, gossip-based peer exchange, and agent-to-agent routing.
package p2p

import (
	"sync"
	"time"
)

// NodeID uniquely identifies a relay node in the mesh.
type NodeID string

// PeerInfo describes a peer relay node known to this node.
type PeerInfo struct {
	ID       NodeID     `json:"id"`
	Address  string     `json:"address"`   // "host:port" for HTTP calls
	LastSeen time.Time  `json:"last_seen"`
	Agents   []AgentRef `json:"agents"`
}

// AgentRef references an agent connected to a specific relay.
type AgentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AgentInfo is the public view of an agent on the mesh.
type AgentInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	NodeID NodeID `json:"node_id"`
	Online bool   `json:"online"`
}

// RoutingTable maps agent IDs to the relay node they're connected to.
type RoutingTable struct {
	mu      sync.RWMutex
	entries map[string]NodeID // agent_id → node_id
}

// NewRoutingTable creates an empty routing table.
func NewRoutingTable() *RoutingTable {
	return &RoutingTable{
		entries: make(map[string]NodeID),
	}
}

// Set records which node an agent is on.
func (rt *RoutingTable) Set(agentID string, nodeID NodeID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.entries[agentID] = nodeID
}

// Remove deletes an agent from the routing table.
func (rt *RoutingTable) Remove(agentID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.entries, agentID)
}

// Lookup returns the node ID for an agent, or empty string if unknown.
func (rt *RoutingTable) Lookup(agentID string) NodeID {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.entries[agentID]
}

// List returns all known agent→node mappings.
func (rt *RoutingTable) List() map[string]NodeID {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make(map[string]NodeID, len(rt.entries))
	for k, v := range rt.entries {
		out[k] = v
	}
	return out
}

// PeerStore manages known peer relays with periodic pruning.
type PeerStore struct {
	mu    sync.RWMutex
	peers map[NodeID]*PeerInfo
}

// NewPeerStore creates an empty peer store.
func NewPeerStore() *PeerStore {
	return &PeerStore{
		peers: make(map[NodeID]*PeerInfo),
	}
}

// Get returns a peer by ID, or nil.
func (ps *PeerStore) Get(id NodeID) *PeerInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.peers[id]
}

// Set adds or updates a peer.
func (ps *PeerStore) Set(p *PeerInfo) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p.LastSeen = time.Now()
	ps.peers[p.ID] = p
}

// Remove deletes a peer.
func (ps *PeerStore) Remove(id NodeID) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.peers, id)
}

// List all peers (snapshot).
func (ps *PeerStore) List() []*PeerInfo {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	out := make([]*PeerInfo, 0, len(ps.peers))
	for _, p := range ps.peers {
		out = append(out, p)
	}
	return out
}

// Prune removes peers not seen within the given duration.
func (ps *PeerStore) Prune(maxAge time.Duration) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, p := range ps.peers {
		if p.LastSeen.Before(cutoff) {
			delete(ps.peers, id)
		}
	}
}

// UpdateAgents updates the agent list for a peer.
func (ps *PeerStore) UpdateAgents(nodeID NodeID, agents []AgentRef) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if p, ok := ps.peers[nodeID]; ok {
		p.Agents = agents
		p.LastSeen = time.Now()
	}
}
