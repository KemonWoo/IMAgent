// Package transport handles WebSocket connections for Agent and APK peers,
// plus P2P mesh HTTP endpoints (V3: node discovery, AI-to-AI routing).
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/KemonWoo/IMAgent/internal/mcp"
	"github.com/KemonWoo/IMAgent/internal/p2p"
	"github.com/KemonWoo/IMAgent/internal/session"
)

// Relay ties together MCP server, session manager, WebSocket transport, and P2P mesh.
type Relay struct {
	sessions   *session.Manager
	mcpSrv     *mcp.Server
	state      *mcp.AppState
	uploadsDir string
	logs       *ringBuffer
	mu         sync.Mutex

	// V3: P2P mesh
	nodeID    p2p.NodeID
	address   string // "host:port" for HTTP mesh calls
	routing   *p2p.RoutingTable
	peers     *p2p.PeerStore
	gossiper  *p2p.Gossiper
	forwarder *p2p.Forwarder
}

// NewRelay creates a new relay instance.
// p2pNodeID and p2pAddress are empty if P2P is disabled.
func NewRelay(uploadsDir, p2pNodeID, p2pAddress string) *Relay {
	state := &mcp.AppState{}
	sessions := session.NewManager()
	logBuf := newRingBuffer(200)

	log.SetOutput(logBuf)

	r := &Relay{
		sessions:   sessions,
		state:      state,
		uploadsDir: uploadsDir,
		logs:       logBuf,
		nodeID:     p2p.NodeID(p2pNodeID),
		address:    p2pAddress,
		routing:    p2p.NewRoutingTable(),
		peers:      p2p.NewPeerStore(),
	}

	// MCP push callback: agent → APK
	r.mcpSrv = mcp.NewServer(state, func(msg []byte) error {
		return r.sessions.RouteFromAgent(msg)
	})

	// V3: Wire P2P callbacks into MCP server
	if p2pNodeID != "" {
		// Gossiper for peer discovery
		r.gossiper = p2p.NewGossiper(r.nodeID, r.address, r.peers, r.routing)
		r.gossiper.Start()

		// Forwarder for agent-to-agent routing
		r.forwarder = p2p.NewForwarder(r.nodeID, r.routing, r.peers, func(agentID string, msg []byte) error {
			return r.sessions.RouteToAgent(agentID, msg)
		})

		// Wire session changes → gossip
		r.sessions.OnAgentsChange(func(agents []p2p.AgentRef) {
			// Update local routing table
			for _, a := range agents {
				r.routing.Set(a.ID, r.nodeID)
			}
			// Gossip to peers
			r.gossiper.GossipAgents(agents)
		})

		// Wire MCP mesh callbacks
		r.mcpSrv.SetMeshCallbacks(&mcp.MeshCallbacks{
			ListAgents: r.listAllAgents,
			ChatAgent:  r.chatToAgent,
			Broadcast:  r.broadcastToAll,
		})
	}

	return r
}

// BootstrapPeers connects to initial mesh peers.
func (r *Relay) BootstrapPeers(addrs []string) {
	if r.gossiper != nil {
		r.gossiper.Bootstrap(addrs)
	}
}

// Stop shuts down P2P components.
func (r *Relay) Stop() {
	if r.gossiper != nil {
		r.gossiper.Stop()
	}
}

// ---------- Mesh callbacks for MCP server ----------

func (r *Relay) listAllAgents() []mcp.AgentInfo {
	var agents []mcp.AgentInfo

	// Local agents
	for _, a := range r.sessions.ListAgents() {
		agents = append(agents, mcp.AgentInfo{
			ID:     a.ID,
			Name:   a.Name,
			NodeID: string(r.nodeID),
			Online: true,
		})
	}

	// Remote agents from peers
	for _, peer := range r.peers.List() {
		for _, a := range peer.Agents {
			// Don't duplicate if already in local list
			dup := false
			for _, existing := range agents {
				if existing.ID == a.ID {
					dup = true
					break
				}
			}
			if !dup {
				agents = append(agents, mcp.AgentInfo{
					ID:     a.ID,
					Name:   a.Name,
					NodeID: string(peer.ID),
					Online: true,
				})
			}
		}
	}

	return agents
}

func (r *Relay) chatToAgent(senderID, targetID, message string) (string, error) {
	// Build agent-to-agent message
	msg, _ := json.Marshal(map[string]interface{}{
		"type":    "agent_chat",
		"from":    "mesh",
		"target":  targetID,
		"content": message,
	})

	if err := r.forwarder.RouteMessage(targetID, msg); err != nil {
		return "failed", err
	}
	return "routed", nil
}

func (r *Relay) broadcastToAll(senderID, message string) (int, error) {
	count := 0

	// Deliver to all local agents
	msg, _ := json.Marshal(map[string]interface{}{
		"type":    "agent_broadcast",
		"from":    "mesh",
		"content": message,
	})
	for _, a := range r.sessions.ListAgents() {
		if a.ID == senderID {
			continue // skip sender
		}
		if err := a.Send(msg); err == nil {
			count++
		}
	}

	// Forward to all remote agents via their relays
	for _, peer := range r.peers.List() {
		for _, agent := range peer.Agents {
			if err := r.forwarder.RouteMessage(agent.ID, msg); err == nil {
				count++
			}
		}
	}

	return count, nil
}

// ---------- Agent WebSocket handler (V3: multi-agent) ----------

func (r *Relay) HandleAgentWS(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("Agent WS accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "agent disconnected")

	ctx := context.Background()

	// V3: unique agent ID per connection
	agentID := fmt.Sprintf("agent-%d-%d", time.Now().UnixMilli(), time.Now().UnixNano()%10000)

	// Write channel to serialize concurrent writes.
	writeCh := make(chan []byte, 64)
	peer := &session.Peer{
		ID:   agentID,
		Name: agentID, // default name, can be overridden later
		Role: session.RoleAgent,
		Send: func(msg []byte) error {
			select {
			case writeCh <- msg:
				return nil
			default:
				return fmt.Errorf("agent write buffer full")
			}
		},
	}
	r.sessions.RegisterAgent(peer)

	// Update local routing table
	if r.nodeID != "" {
		r.routing.Set(agentID, r.nodeID)
	}

	log.Printf("Agent connected: %s (total: %d)", agentID, r.sessions.AgentCount())

	// Writer goroutine
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for msg := range writeCh {
			if err := wsjson.Write(ctx, conn, json.RawMessage(msg)); err != nil {
				log.Printf("Agent %s write: %v", agentID, err)
				return
			}
		}
	}()

	// MCP message loop
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			log.Printf("Agent %s read: %v", agentID, err)
			break
		}

		var mcpReq mcp.JSONRPCRequest
		if err := json.Unmarshal(raw, &mcpReq); err != nil {
			log.Printf("Agent %s JSON parse: %v", agentID, err)
			continue
		}

		// Set agent context before handling
		r.mcpSrv.SetAgentID(agentID)
		resp := r.mcpSrv.Handle(mcpReq)

		// Update session code if voice_connect was called
		if mcpReq.Method == "tools/call" {
			_, code, _, _ := r.state.GetState()
			if code != "" {
				r.sessions.SetCode(code)
			}
		}

		respBytes, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Agent %s marshal: %v", agentID, err)
			continue
		}
		select {
		case writeCh <- respBytes:
		default:
			log.Printf("Agent %s write buffer full, dropping response", agentID)
		}
	}

	// Cleanup
	close(writeCh)
	<-writeDone

	r.sessions.UnregisterAgent(agentID)
	if r.nodeID != "" {
		r.routing.Remove(agentID)
	}
	log.Printf("Agent disconnected: %s (total: %d)", agentID, r.sessions.AgentCount())
}

// ---------- APK WebSocket handler (unchanged from V2) ----------

func (r *Relay) HandleAPKWS(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("APK WS accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "apk disconnected")

	ctx := context.Background()
	connected := false

	handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, raw, err := conn.Read(handshakeCtx)
	if err != nil {
		log.Printf("APK handshake read: %v", err)
		wsjson.Write(ctx, conn, map[string]string{"status": "timeout"})
		return
	}

	var hs struct {
		Role string `json:"role"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &hs); err != nil {
		wsjson.Write(ctx, conn, map[string]string{"status": "bad_request"})
		return
	}

	if hs.Role != "apk" {
		wsjson.Write(ctx, conn, map[string]string{"status": "unknown_role"})
		return
	}

	if !r.sessions.HasAgent() {
		wsjson.Write(ctx, conn, map[string]string{"status": "no_agent", "message": "Agent not connected yet."})
		return
	}

	if !r.sessions.VerifyCode(hs.Code) {
		wsjson.Write(ctx, conn, map[string]string{"status": "code_mismatch"})
		return
	}

	wsjson.Write(ctx, conn, map[string]string{"status": "connected"})
	connected = true
	log.Printf("APK connected")

	writeCh := make(chan []byte, 64)
	peer := &session.Peer{
		ID:   "apk",
		Role: session.RoleAPK,
		Send: func(msg []byte) error {
			select {
			case writeCh <- msg:
				return nil
			default:
				return fmt.Errorf("apk write buffer full")
			}
		},
	}
	r.sessions.RegisterAPK(peer)
	r.state.SetAPKConnected(true)

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for msg := range writeCh {
			if err := wsjson.Write(ctx, conn, json.RawMessage(msg)); err != nil {
				log.Printf("APK write: %v", err)
				return
			}
		}
	}()

	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			log.Printf("APK read: %v", err)
			break
		}

		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		msg["from"] = "apk"
		r.sessions.RouteFromAPK(raw)
	}

	close(writeCh)
	<-writeDone

	r.sessions.UnregisterAPK()
	r.state.SetAPKConnected(false)
	log.Printf("APK disconnected (was_connected=%v)", connected)
}

// ---------- P2P HTTP handlers (V3) ----------

// HandleP2PAnnounce handles POST /p2p/announce
func (r *Relay) HandleP2PAnnounce(w http.ResponseWriter, req *http.Request) {
	if r.gossiper == nil {
		http.Error(w, "P2P not enabled", http.StatusServiceUnavailable)
		return
	}
	r.gossiper.HandleAnnounce(w, req)
}

// HandleP2PPeers handles GET /p2p/peers
func (r *Relay) HandleP2PPeers(w http.ResponseWriter, req *http.Request) {
	if r.gossiper == nil {
		http.Error(w, "P2P not enabled", http.StatusServiceUnavailable)
		return
	}
	r.gossiper.HandlePeers(w, req)
}

// HandleP2PAgents handles GET /p2p/agents
func (r *Relay) HandleP2PAgents(w http.ResponseWriter, req *http.Request) {
	if r.gossiper == nil {
		http.Error(w, "P2P not enabled", http.StatusServiceUnavailable)
		return
	}
	r.gossiper.HandleAgents(w, req)
}

// HandleP2PSync handles POST /p2p/sync
func (r *Relay) HandleP2PSync(w http.ResponseWriter, req *http.Request) {
	if r.gossiper == nil {
		http.Error(w, "P2P not enabled", http.StatusServiceUnavailable)
		return
	}
	r.gossiper.HandleSync(w, req)
}

// HandleP2PForward handles POST /p2p/forward
func (r *Relay) HandleP2PForward(w http.ResponseWriter, req *http.Request) {
	if r.forwarder == nil {
		http.Error(w, "P2P not enabled", http.StatusServiceUnavailable)
		return
	}
	r.forwarder.HandleForward(w, req)
}

// ---------- File upload handler ----------

func (r *Relay) HandleUpload(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST required"})
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, 50<<20)

	if err := req.ParseMultipartForm(32 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("parse: %v", err)})
		return
	}

	file, header, err := req.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing 'file' field"})
		return
	}
	defer file.Close()

	origName := filepath.Base(header.Filename)
	ext := filepath.Ext(origName)
	safeName := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, origName[:len(origName)-len(ext)])) + ext

	os.MkdirAll(r.uploadsDir, 0755)
	dstPath := filepath.Join(r.uploadsDir, safeName)
	dst, err := os.Create(dstPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "save failed"})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(dstPath)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "write failed"})
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(ext)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	fileType := classifyFile(mimeType, ext)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"name":     safeName,
		"original": origName,
		"size":     written,
		"mime":     mimeType,
		"type":     fileType,
		"url":      fmt.Sprintf("/dl/%s", safeName),
	})
}

func classifyFile(mimeType, ext string) string {
	if strings.HasPrefix(mimeType, "image/") {
		return "image"
	}
	if strings.HasPrefix(mimeType, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video"
	}
	switch strings.ToLower(ext) {
	case ".pdf":
		return "document"
	case ".doc", ".docx", ".txt", ".md":
		return "document"
	case ".apk":
		return "apk"
	}
	return "file"
}
