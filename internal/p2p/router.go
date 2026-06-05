// Package p2p — message forwarding (agent-to-agent routing). V2: TLS support.
package p2p

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Forwarder handles agent-to-agent message routing across the mesh.
type Forwarder struct {
	nodeID  NodeID
	routing *RoutingTable
	peers   *PeerStore
	client  *http.Client
	scheme  string // "http" or "https"
	// deliverLocal is called to deliver a message to a local agent via its WebSocket.
	deliverLocal func(agentID string, msg []byte) error
}

// NewForwarder creates a message forwarder.
func NewForwarder(nodeID NodeID, routing *RoutingTable, peers *PeerStore, deliverLocal func(agentID string, msg []byte) error) *Forwarder {
	return &Forwarder{
		nodeID:       nodeID,
		routing:      routing,
		peers:        peers,
		client:       &http.Client{Timeout: 10 * time.Second},
		scheme:       "http",
		deliverLocal: deliverLocal,
	}
}

// SetScheme changes the URL scheme for forwarding calls (V2 TLS support).
func (f *Forwarder) SetScheme(s string) {
	f.scheme = s
	if s == "https" {
		f.client = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
}

// RouteMessage routes a message to a target agent.
func (f *Forwarder) RouteMessage(targetAgentID string, msg []byte) error {
	nodeID := f.routing.Lookup(targetAgentID)

	if nodeID == "" {
		return fmt.Errorf("agent %s not found in routing table", targetAgentID)
	}

	if nodeID == f.nodeID {
		return f.deliverLocal(targetAgentID, msg)
	}

	peer := f.peers.Get(nodeID)
	if peer == nil {
		return fmt.Errorf("peer %s not found", nodeID)
	}

	return f.forwardHTTP(peer.Address, targetAgentID, msg)
}

func (f *Forwarder) forwardHTTP(peerAddr, targetAgentID string, msg []byte) error {
	body, _ := json.Marshal(map[string]interface{}{
		"from_node": string(f.nodeID),
		"to_agent":  targetAgentID,
		"message":   json.RawMessage(msg),
	})

	resp, err := f.client.Post(
		fmt.Sprintf("%s://%s/p2p/forward", f.scheme, peerAddr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		log.Printf("P2P forward to %s: %v", peerAddr, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("forward failed: %d %s", resp.StatusCode, string(body))
	}
	return nil
}

// HandleForward handles POST /p2p/forward.
func (f *Forwarder) HandleForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		FromNode string          `json:"from_node"`
		ToAgent  string          `json:"to_agent"`
		Message  json.RawMessage `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.ToAgent == "" {
		http.Error(w, "to_agent required", http.StatusBadRequest)
		return
	}

	log.Printf("P2P forward: %s → agent %s (from node %s)", f.nodeID, req.ToAgent, req.FromNode)

	if err := f.deliverLocal(req.ToAgent, req.Message); err != nil {
		log.Printf("P2P forward deliver: %v", err)
		http.Error(w, fmt.Sprintf("deliver failed: %v", err), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "delivered"})
}
