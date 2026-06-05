// Package mcp implements a minimal MCP server for IMAgent relay.
// Supports: initialize, tools/list, tools/call (voice_* + V3 agent_* tools).
package mcp

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// JSONRPCRequest is a standard JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a standard JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error represents a JSON-RPC error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool definition.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema for a tool.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Property describes a tool parameter.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// AppState holds the runtime state accessible to MCP tools.
type AppState struct {
	mu           sync.RWMutex
	NexusID      string
	Code         string
	APKConnected bool
	LastSpeak    string
}

func (s *AppState) SetNexus(id, code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NexusID = id
	s.Code = code
}

func (s *AppState) SetAPKConnected(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.APKConnected = v
}

func (s *AppState) SetLastSpeak(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastSpeak = text
}

func (s *AppState) GetState() (nexusID string, code string, apkConnected bool, lastSpeak string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.NexusID, s.Code, s.APKConnected, s.LastSpeak
}

// Callback for pushing messages to APK.
type PushFunc func(msg []byte) error

// OnVoiceConnect is called after voice_connect succeeds. Returns QR-related info.
type OnVoiceConnect func(agentID, code string) (qrURL, relayAddr string)

// MeshCallbacks provides the MCP server access to the P2P mesh.
type MeshCallbacks struct {
	// ListAgents returns all known agents across the mesh.
	ListAgents func() []AgentInfo
	// ChatAgent sends a message to a specific agent. senderID is excluded from delivery.
	ChatAgent func(senderID, targetAgentID, message string) (status string, err error)
	// Broadcast sends a message to all known agents. senderID is excluded from delivery.
	Broadcast func(senderID, message string) (count int, err error)
}

// AgentInfo describes an agent on the mesh.
type AgentInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	NodeID string `json:"node_id"`
	Online bool   `json:"online"`
}

// Server handles MCP protocol messages.
type Server struct {
	state          *AppState
	push           PushFunc
	mesh           *MeshCallbacks
	currentAgentID string // set before each Handle call
	version        string // relay version string
	tools          []Tool
	publicAddr     string // relay public address for QR generation
	onVoiceConnect OnVoiceConnect
}

// SetAgentID sets the current agent ID for this request context.
func (s *Server) SetAgentID(id string) {
	s.currentAgentID = id
}

// SetPublicAddr sets the relay's public address for QR pairing.
func (s *Server) SetPublicAddr(addr string) {
	s.publicAddr = addr
}

// SetVersion sets the relay version string.
func (s *Server) SetVersion(v string) {
	s.version = v
}

// SetOnVoiceConnect sets the callback for post-voice_connect QR generation.
func (s *Server) SetOnVoiceConnect(cb OnVoiceConnect) {
	s.onVoiceConnect = cb
}

// NewServer creates a new MCP server.
func NewServer(state *AppState, push PushFunc) *Server {
	return &Server{
		state: state,
		push:  push,
		tools: []Tool{
			{
				Name:        "voice_connect",
				Description: "Register this Agent with the relay and generate a pairing code. The human enters this code on their phone APK to connect.",
				InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
			},
			{
				Name:        "voice_status",
				Description: "Check whether the phone APK is currently connected.",
				InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
			},
			{
				Name:        "voice_speak",
				Description: "Send text to the phone APK for TTS speech playback.",
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"text": {Type: "string", Description: "The text to speak on the phone."},
					},
					Required: []string{"text"},
				},
			},
			{
				Name:        "voice_chat",
				Description: "Send a text message to display in the phone APK chat UI.",
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"content": {Type: "string", Description: "The message content to display."},
					},
					Required: []string{"content"},
				},
			},
			{
				Name:        "voice_file",
				Description: "Send a file notification to the phone APK. The file should already be uploaded via HTTP POST /upload. Provide the download URL, filename, size, and MIME type.",
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"url":  {Type: "string", Description: "Download URL of the file (e.g. /dl/filename.png)"},
						"name": {Type: "string", Description: "Original filename"},
						"size": {Type: "integer", Description: "File size in bytes"},
						"mime": {Type: "string", Description: "MIME type (e.g. image/png)"},
						"type": {Type: "string", Description: "File category: image, audio, video, document, file"},
					},
					Required: []string{"url", "name", "size", "mime", "type"},
				},
			},
			{
				Name:        "voice_reset",
				Description: "Disconnect the current phone APK pairing and generate a new code.",
				InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
			},
			// V3: AI Community tools
			{
				Name:        "agent_list",
				Description: "List all AI agents currently known across the mesh network. Returns agent ID, name, node, and online status.",
				InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
			},
			{
				Name:        "agent_chat",
				Description: "Send a message to another AI agent on the mesh. Target an agent by ID. The message is routed through the relay mesh to reach the destination agent.",
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"target":  {Type: "string", Description: "Target agent ID to message."},
						"message": {Type: "string", Description: "Message content to send to the target agent."},
					},
					Required: []string{"target", "message"},
				},
			},
			{
				Name:        "agent_broadcast",
				Description: "Broadcast a message to ALL known AI agents on the mesh network.",
				InputSchema: InputSchema{
					Type: "object",
					Properties: map[string]Property{
						"message": {Type: "string", Description: "Message content to broadcast to all agents."},
					},
					Required: []string{"message"},
				},
			},
		},
	}
}

// SetMeshCallbacks wires the P2P mesh capabilities into the MCP server.
func (s *Server) SetMeshCallbacks(cb *MeshCallbacks) {
	s.mesh = cb
}

// Handle processes an incoming MCP request and returns a response.
func (s *Server) Handle(request JSONRPCRequest) JSONRPCResponse {
	switch request.Method {
	case "initialize":
		return s.handleInitialize(request)
	case "tools/list":
		return s.handleToolsList(request)
	case "tools/call":
		return s.handleToolsCall(request)
	default:
		return s.errorResponse(request.ID, -32601, fmt.Sprintf("unknown method: %s", request.Method))
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]string{
				"name":    "imagent-relay",
				"version": s.version,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	}
}

func (s *Server) handleToolsList(req JSONRPCRequest) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": s.tools,
		},
	}
}

func (s *Server) handleToolsCall(req JSONRPCRequest) JSONRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, -32602, "invalid params")
	}

	var resultContent []map[string]any

	switch params.Name {
	case "voice_connect":
		code := generateCode()
		id := s.currentAgentID
		if id == "" {
			id = fmt.Sprintf("agent-%d", time.Now().UnixMilli())
		}
		s.state.SetNexus(id, code)

		msg := fmt.Sprintf("Pairing code: %s", code)

		// Generate QR if callback is set
		if s.onVoiceConnect != nil {
			qrURL, _ := s.onVoiceConnect(id, code)
			if qrURL != "" {
				msg += fmt.Sprintf("\nQR code: %s", qrURL)
				msg += fmt.Sprintf("\nimagent://pair?r=%s&c=%s", s.publicAddr, code)
			}
		}

		msg += "\n\nShare this code (or QR) with the human. They scan or enter it on the phone APK."

		resultContent = []map[string]any{
			{"type": "text", "text": msg},
		}

	case "voice_status":
		_, _, connected, _ := s.state.GetState()
		status := "offline"
		if connected {
			status = "online"
		}
		resultContent = []map[string]any{
			{"type": "text", "text": fmt.Sprintf("Phone APK: %s", status)},
		}

	case "voice_speak":
		var args struct {
			Text string `json:"text"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Text == "" {
			return s.errorResponse(req.ID, -32602, "text is required")
		}
		s.state.SetLastSpeak(args.Text)
		msg, _ := json.Marshal(map[string]string{
			"type":    "tts",
			"content": args.Text,
		})
		if s.push != nil {
			s.push(msg)
		}
		resultContent = []map[string]any{
			{"type": "text", "text": "Sent to phone."},
		}

	case "voice_chat":
		var args struct {
			Content string `json:"content"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.Content == "" {
			return s.errorResponse(req.ID, -32602, "content is required")
		}
		msg, _ := json.Marshal(map[string]any{
			"type":    "chat_response",
			"content": args.Content,
		})
		if s.push != nil {
			s.push(msg)
		}
		resultContent = []map[string]any{
			{"type": "text", "text": "Sent to phone."},
		}

	case "voice_file":
		var args struct {
			URL  string `json:"url"`
			Name string `json:"name"`
			Size int64  `json:"size"`
			Mime string `json:"mime"`
			Type string `json:"type"`
		}
		json.Unmarshal(params.Arguments, &args)
		if args.URL == "" || args.Name == "" {
			return s.errorResponse(req.ID, -32602, "url and name are required")
		}
		msg, _ := json.Marshal(map[string]any{
			"type": "file",
			"file": map[string]any{
				"url":  args.URL,
				"name": args.Name,
				"size": args.Size,
				"mime": args.Mime,
				"type": args.Type,
			},
		})
		if s.push != nil {
			s.push(msg)
		}
		resultContent = []map[string]any{
			{"type": "text", "text": "File notification sent to phone."},
		}

	case "voice_reset":
		s.state.SetNexus("", "")
		s.state.SetAPKConnected(false)
		msg, _ := json.Marshal(map[string]string{
			"type": "reset",
		})
		if s.push != nil {
			s.push(msg)
		}
		resultContent = []map[string]any{
			{"type": "text", "text": "Disconnected. Call voice_connect to re-pair."},
		}

	// V3: AI Community tools
	case "agent_list":
		resultContent = s.handleAgentList()

	case "agent_chat":
		resultContent = s.handleAgentChat(params.Arguments, req.ID)

	case "agent_broadcast":
		resultContent = s.handleAgentBroadcast(params.Arguments, req.ID)

	default:
		return s.errorResponse(req.ID, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": resultContent,
		},
	}
}

func (s *Server) handleAgentList() []map[string]any {
	if s.mesh == nil || s.mesh.ListAgents == nil {
		return []map[string]any{
			{"type": "text", "text": "Mesh networking not enabled. Start relay with --p2p flag."},
		}
	}
	agents := s.mesh.ListAgents()
	if len(agents) == 0 {
		return []map[string]any{
			{"type": "text", "text": "No other agents discovered on the mesh yet."},
		}
	}

	// Build a readable list
	text := fmt.Sprintf("Known agents on mesh (%d):\n", len(agents))
	for _, a := range agents {
		text += fmt.Sprintf("  • %s (name: %s, node: %s, %s)\n",
			a.ID, a.Name, a.NodeID, onlineText(a.Online))
	}

	return []map[string]any{
		{"type": "text", "text": text},
	}
}

func (s *Server) handleAgentChat(argsRaw json.RawMessage, reqID any) []map[string]any {
	if s.mesh == nil || s.mesh.ChatAgent == nil {
		return []map[string]any{
			{"type": "text", "text": "Mesh networking not enabled. Start relay with --p2p flag."},
		}
	}

	var args struct {
		Target  string `json:"target"`
		Message string `json:"message"`
	}
	json.Unmarshal(argsRaw, &args)

	if args.Target == "" || args.Message == "" {
		return []map[string]any{
			{"type": "text", "text": "target and message are required"},
		}
	}

	status, err := s.mesh.ChatAgent(s.currentAgentID, args.Target, args.Message)
	if err != nil {
		return []map[string]any{
			{"type": "text", "text": fmt.Sprintf("Failed to send: %v", err)},
		}
	}

	return []map[string]any{
		{"type": "text", "text": fmt.Sprintf("Message sent to %s: %s", args.Target, status)},
	}
}

func (s *Server) handleAgentBroadcast(argsRaw json.RawMessage, reqID any) []map[string]any {
	if s.mesh == nil || s.mesh.Broadcast == nil {
		return []map[string]any{
			{"type": "text", "text": "Mesh networking not enabled. Start relay with --p2p flag."},
		}
	}

	var args struct {
		Message string `json:"message"`
	}
	json.Unmarshal(argsRaw, &args)

	if args.Message == "" {
		return []map[string]any{
			{"type": "text", "text": "message is required"},
		}
	}

	count, err := s.mesh.Broadcast(s.currentAgentID, args.Message)
	if err != nil {
		return []map[string]any{
			{"type": "text", "text": fmt.Sprintf("Broadcast error: %v", err)},
		}
	}

	return []map[string]any{
		{"type": "text", "text": fmt.Sprintf("Broadcast sent to %d agents.", count)},
	}
}

func onlineText(online bool) string {
	if online {
		return "online"
	}
	return "offline"
}

func (s *Server) errorResponse(id any, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
}

func generateCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		time.Sleep(1)
	}
	return string(b)
}
