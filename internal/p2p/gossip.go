// Package p2p — gossip protocol for peer discovery and agent sync.
// V4: adaptive reconnection with exponential backoff and circuit breaker.
package p2p

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/KemonWoo/IMAgent/internal/metrics"
)

// Gossiper handles peer discovery and agent synchronization across the mesh.
type Gossiper struct {
	nodeID      NodeID
	address     string
	peers       *PeerStore
	routing     *RoutingTable
	client      *http.Client
	gossipEvery time.Duration
	pruneEvery  time.Duration
	maxPeerAge  time.Duration
	stopCh      chan struct{}

	// V4: adaptive reconnection
	backoffs  map[NodeID]*metrics.Backoff
	breakers  map[NodeID]*metrics.CircuitBreaker
	bkMu      sync.Mutex
}

// NewGossiper creates a new gossiper for this relay node.
func NewGossiper(nodeID NodeID, address string, peers *PeerStore, routing *RoutingTable) *Gossiper {
	return &Gossiper{
		nodeID:      nodeID,
		address:     address,
		peers:       peers,
		routing:     routing,
		client:      &http.Client{Timeout: 5 * time.Second},
		gossipEvery: 30 * time.Second,
		pruneEvery:  120 * time.Second,
		maxPeerAge:  300 * time.Second,
		stopCh:      make(chan struct{}),
		backoffs:    make(map[NodeID]*metrics.Backoff),
		breakers:    make(map[NodeID]*metrics.CircuitBreaker),
	}
}

func (g *Gossiper) getBackoff(id NodeID) *metrics.Backoff {
	g.bkMu.Lock()
	defer g.bkMu.Unlock()
	if b, ok := g.backoffs[id]; ok {
		return b
	}
	b := metrics.NewBackoff()
	g.backoffs[id] = b
	return b
}

func (g *Gossiper) getBreaker(id NodeID) *metrics.CircuitBreaker {
	g.bkMu.Lock()
	defer g.bkMu.Unlock()
	if b, ok := g.breakers[id]; ok {
		return b
	}
	b := metrics.NewCircuitBreaker(5, 300*time.Second) // 5 failures → 5min open
	g.breakers[id] = b
	return b
}

// Start begins periodic gossip and pruning with jitter.
func (g *Gossiper) Start() {
	go g.gossipLoop()
	go g.pruneLoop()
}

// Stop halts gossip loops.
func (g *Gossiper) Stop() {
	close(g.stopCh)
}

// Bootstrap connects to a list of initial peers with adaptive retry.
func (g *Gossiper) Bootstrap(bootstrapAddrs []string) {
	for _, addr := range bootstrapAddrs {
		if addr == g.address {
			continue
		}
		go g.bootstrapPeer(addr)
	}
}

func (g *Gossiper) bootstrapPeer(addr string) {
	id := NodeID(addr) // temporary ID based on address
	bo := g.getBackoff(id)
	cb := g.getBreaker(id)

	for {
		select {
		case <-g.stopCh:
			return
		default:
		}

		if !cb.Allow() {
			log.Printf("P2P bootstrap %s: circuit open, waiting %v", addr, cb.ResetAfter())
			time.Sleep(cb.ResetAfter())
			continue
		}

		if err := g.announce(addr); err != nil {
			log.Printf("P2P bootstrap %s failed (attempt %d): %v", addr, bo.Attempts()+1, err)
			cb.RecordFailure()
			d := bo.Next()
			log.Printf("P2P bootstrap %s: retry in %v", addr, d)
			time.Sleep(d)
			continue
		}

		// Success
		bo.Reset()
		cb.RecordSuccess()
		log.Printf("P2P bootstrap %s: connected", addr)
		// Peer is now known by its real ID — stop bootstrap retry
		return
	}
}

// GossipAgents pushes our local agent list to all known peers.
func (g *Gossiper) GossipAgents(agents []AgentRef) {
	for _, peer := range g.peers.List() {
		go g.pushAgents(peer.Address, agents)
	}
}

func (g *Gossiper) gossipLoop() {
	ticker := time.NewTicker(g.gossipEvery)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			for _, peer := range g.peers.List() {
				go g.exchangePeers(peer.Address)
			}
		}
	}
}

func (g *Gossiper) pruneLoop() {
	ticker := time.NewTicker(g.pruneEvery)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.peers.Prune(g.maxPeerAge)
		}
	}
}

// announce tells a remote relay about us and learns about its peers.
// Returns error for callers to handle retry logic.
func (g *Gossiper) announce(remoteAddr string) error {
	body, _ := json.Marshal(map[string]string{
		"id":      string(g.nodeID),
		"address": g.address,
	})
	resp, err := g.client.Post(
		fmt.Sprintf("http://%s/p2p/announce", remoteAddr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("announce POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("announce status %d", resp.StatusCode)
	}

	var result struct {
		Peers []*PeerInfo `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("announce parse: %w", err)
	}

	for _, p := range result.Peers {
		if p.ID == g.nodeID {
			continue
		}
		g.peers.Set(p)
		for _, a := range p.Agents {
			g.routing.Set(a.ID, p.ID)
		}
	}
	return nil
}

// exchangePeers fetches the peer list from a remote relay.
func (g *Gossiper) exchangePeers(remoteAddr string) {
	cb := g.getBreaker(NodeID(remoteAddr))
	if !cb.Allow() {
		return
	}

	resp, err := g.client.Get(fmt.Sprintf("http://%s/p2p/peers", remoteAddr))
	if err != nil {
		cb.RecordFailure()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cb.RecordFailure()
		return
	}

	var result struct {
		Peers []*PeerInfo `json:"peers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		cb.RecordFailure()
		return
	}

	cb.RecordSuccess()

	for _, p := range result.Peers {
		if p.ID == g.nodeID {
			continue
		}
		g.peers.Set(p)
		for _, a := range p.Agents {
			g.routing.Set(a.ID, p.ID)
		}
	}
}

// pushAgents sends our local agent list to a peer.
func (g *Gossiper) pushAgents(remoteAddr string, agents []AgentRef) {
	body, _ := json.Marshal(map[string]interface{}{
		"node_id": string(g.nodeID),
		"agents":  agents,
	})
	resp, err := g.client.Post(
		fmt.Sprintf("http://%s/p2p/sync", remoteAddr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// jitterDuration adds ±20% jitter to a duration.
func jitterDuration(d time.Duration) time.Duration {
	j := time.Duration(float64(d) * 0.2 * (rand.Float64()*2 - 1))
	return d + j
}

// ---------- HTTP handlers ----------

// HandleAnnounce handles POST /p2p/announce — another relay announces itself.
func (g *Gossiper) HandleAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.ID == "" || req.Address == "" {
		http.Error(w, "id and address required", http.StatusBadRequest)
		return
	}

	peerID := NodeID(req.ID)
	g.peers.Set(&PeerInfo{
		ID:      peerID,
		Address: req.Address,
	})

	log.Printf("P2P: new peer %s at %s", req.ID, req.Address)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": g.peers.List(),
	})

	go g.exchangePeers(req.Address)
}

// HandlePeers handles GET /p2p/peers — return known peers.
func (g *Gossiper) HandlePeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"peers": g.peers.List(),
	})
}

// HandleAgents handles GET /p2p/agents — return all known agents across mesh.
func (g *Gossiper) HandleAgents(w http.ResponseWriter, r *http.Request) {
	localAgents := g.routing.List()

	type agentEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		NodeID string `json:"node_id"`
		Online bool   `json:"online"`
	}
	agents := make([]agentEntry, 0)

	for agentID, nodeID := range localAgents {
		agents = append(agents, agentEntry{
			ID:     agentID,
			NodeID: string(nodeID),
			Online: true,
		})
	}

	for _, peer := range g.peers.List() {
		for _, a := range peer.Agents {
			agents = append(agents, agentEntry{
				ID:     a.ID,
				Name:   a.Name,
				NodeID: string(peer.ID),
				Online: true,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"agents": agents,
	})
}

// HandleSync handles POST /p2p/sync — peer pushes its agent list.
func (g *Gossiper) HandleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NodeID string     `json:"node_id"`
		Agents []AgentRef `json:"agents"`
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &req)

	g.peers.UpdateAgents(NodeID(req.NodeID), req.Agents)
	for _, a := range req.Agents {
		g.routing.Set(a.ID, NodeID(req.NodeID))
	}

	w.WriteHeader(http.StatusOK)
}
