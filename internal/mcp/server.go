// Package mcp implements a minimal MCP server for IMAgent relay.
// Supports: initialize, tools/list, tools/call (voice_connect, voice_speak, voice_reset).
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
	Name        string     `json:"name"`
	Description string     `json:"description"`
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
	mu         sync.RWMutex
	NexusID    string
	Code       string
	APKConnected bool
	LastSpeak  string
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

// Server handles MCP protocol messages.
type Server struct {
	state  *AppState
	push   PushFunc
	tools  []Tool
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
				Name:        "voice_reset",
				Description: "Disconnect the current phone APK pairing and generate a new code.",
				InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
			},
		},
	}
}

// Handle processes an incoming MCP request and returns a response.
// If push is needed (e.g. voice_speak), it calls the push function.
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
				"version": "1.0.0",
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
		// Generate a deterministic 4-char code for V1
		code := generateCode()
		id := fmt.Sprintf("agent-%d", time.Now().UnixMilli())
		s.state.SetNexus(id, code)
		resultContent = []map[string]any{
			{"type": "text", "text": fmt.Sprintf("Pairing code: %s\nNexus ID: %s\nShare this code with the human. They enter it on the phone APK.", code, id)},
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
		// Push to APK
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

	case "voice_reset":
		s.state.SetNexus("", "")
		s.state.SetAPKConnected(false)
		// Notify APK
		msg, _ := json.Marshal(map[string]string{
			"type":   "reset",
		})
		if s.push != nil {
			s.push(msg)
		}
		resultContent = []map[string]any{
			{"type": "text", "text": "Disconnected. Call voice_connect to re-pair."},
		}

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

func (s *Server) errorResponse(id any, code int, message string) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
}

func generateCode() string {
	// Simple 4-char uppercase alphanumeric code
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
		time.Sleep(1) // ensure entropy
	}
	return string(b)
}
