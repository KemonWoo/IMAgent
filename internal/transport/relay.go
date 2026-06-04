// Package transport handles WebSocket connections for Agent and APK peers.
package transport

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"

	"github.com/KemonWoo/IMAgent/internal/mcp"
	"github.com/KemonWoo/IMAgent/internal/session"
)

// Relay ties together MCP server, session manager, and WebSocket transport.
type Relay struct {
	sessions *session.Manager
	mcpSrv   *mcp.Server
	state    *mcp.AppState
	mu       sync.Mutex
}

// NewRelay creates a new relay instance.
func NewRelay() *Relay {
	state := &mcp.AppState{}
	sessions := session.NewManager()

	r := &Relay{
		sessions: sessions,
		state:    state,
	}
	// MCP push callback sends to APK via session manager
	r.mcpSrv = mcp.NewServer(state, func(msg []byte) error {
		return r.sessions.RouteFromAgent(msg)
	})
	return r
}

// HandleAgentWS is the WebSocket handler for Agent (MCP) connections.
func (r *Relay) HandleAgentWS(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("Agent WS accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "agent disconnected")

	ctx := context.Background()
	peer := &session.Peer{
		ID:   "agent",
		Role: session.RoleAgent,
		Send: func(msg []byte) error {
			return wsjson.Write(ctx, conn, json.RawMessage(msg))
		},
	}
	r.sessions.RegisterAgent(peer)
	log.Printf("Agent connected")

	// MCP message loop
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			log.Printf("Agent read: %v", err)
			break
		}

		var req mcp.JSONRPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			log.Printf("Agent JSON parse: %v", err)
			continue
		}

		resp := r.mcpSrv.Handle(req)

		// Update session code if voice_connect was called
		if req.Method == "tools/call" {
			_, code, _, _ := r.state.GetState()
			if code != "" {
				r.sessions.SetCode(code)
			}
		}

		if err := wsjson.Write(ctx, conn, resp); err != nil {
			log.Printf("Agent write: %v", err)
			break
		}
	}
}

// HandleAPKWS is the WebSocket handler for the phone APK.
func (r *Relay) HandleAPKWS(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("APK WS accept: %v", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "apk disconnected")

	ctx := context.Background()
	connected := false

	// Read timeout for initial handshake
	handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Wait for handshake
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

	// V1: auto-accept if no agent, or verify code
	if !r.sessions.HasAgent() {
		wsjson.Write(ctx, conn, map[string]string{"status": "no_agent", "message": "Agent not connected yet."})
		return
	}

	if !r.sessions.VerifyCode(hs.Code) {
		wsjson.Write(ctx, conn, map[string]string{"status": "code_mismatch"})
		return
	}

	// Handshake OK
	wsjson.Write(ctx, conn, map[string]string{"status": "connected"})
	connected = true
	log.Printf("APK connected")

	peer := &session.Peer{
		ID:   "apk",
		Role: session.RoleAPK,
		Send: func(msg []byte) error {
			return wsjson.Write(ctx, conn, json.RawMessage(msg))
		},
	}
	r.sessions.RegisterAPK(peer)
	r.state.SetAPKConnected(true)

	// Message loop
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			log.Printf("APK read: %v", err)
			break
		}

		// Route APK messages to Agent
		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		msg["from"] = "apk"
		r.sessions.RouteFromAPK(raw)
	}

	// Cleanup
	r.sessions.UnregisterAPK()
	r.state.SetAPKConnected(false)
	log.Printf("APK disconnected (was_connected=%v)", connected)
}

// HandleUpload is a placeholder HTTP file upload endpoint (V2).
func (r *Relay) HandleUpload(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(map[string]string{"error": "file upload available in V2"})
}
