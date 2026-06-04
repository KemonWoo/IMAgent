// Package transport handles WebSocket connections for Agent and APK peers.
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
	"github.com/KemonWoo/IMAgent/internal/session"
)

// Relay ties together MCP server, session manager, and WebSocket transport.
type Relay struct {
	sessions   *session.Manager
	mcpSrv     *mcp.Server
	state      *mcp.AppState
	uploadsDir string
	mu         sync.Mutex
}

// NewRelay creates a new relay instance.
func NewRelay(uploadsDir string) *Relay {
	state := &mcp.AppState{}
	sessions := session.NewManager()

	r := &Relay{
		sessions:   sessions,
		state:      state,
		uploadsDir: uploadsDir,
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

	// Write channel to serialize concurrent writes (MCP responses + APK→Agent push).
	writeCh := make(chan []byte, 64)
	peer := &session.Peer{
		ID:   "agent",
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
	log.Printf("Agent connected")

	// Writer goroutine
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for msg := range writeCh {
			if err := wsjson.Write(ctx, conn, json.RawMessage(msg)); err != nil {
				log.Printf("Agent write: %v", err)
				return
			}
		}
	}()

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

		respBytes, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Agent marshal: %v", err)
			continue
		}
		select {
		case writeCh <- respBytes:
		default:
			log.Printf("Agent write buffer full, dropping response")
		}
	}

	// Stop writer goroutine
	close(writeCh)
	<-writeDone

	r.sessions.RegisterAgent(nil) // unregister
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

	// Write channel to serialize concurrent writes (nhooyr/websocket is not
	// safe for concurrent use — conn.Read in this goroutine and conn.Write
	// from MCP push callback would conflict without serialization).
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

	// Writer goroutine
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

	// Stop writer goroutine
	close(writeCh)
	<-writeDone

	// Cleanup
	r.sessions.UnregisterAPK()
	r.state.SetAPKConnected(false)
	log.Printf("APK disconnected (was_connected=%v)", connected)
}

// HandleUpload accepts multipart file uploads. Returns JSON with file URL and metadata.
func (r *Relay) HandleUpload(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "POST required"})
		return
	}

	// Limit to 50MB
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

	// Sanitize filename
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

	// Detect MIME type
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
