//go:build ignore

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Provider config
type LLMConfig struct {
	APIBase string
	Model   string
	APIKey  string
}

var (
	pairCode string
	llmCfg   LLMConfig
)

func main() {
	// All LLM config from env vars — no provider identification in code
	llmCfg.APIBase = os.Getenv("LLM_API_BASE")
	llmCfg.Model  = os.Getenv("LLM_MODEL")
	llmCfg.APIKey = readKey("LLM_API_KEY", "/tmp/llm_key.txt")

	if llmCfg.APIBase == "" || llmCfg.Model == "" {
		log.Fatal("LLM_API_BASE and LLM_MODEL env vars required")
	}
	if llmCfg.APIKey == "" {
		log.Fatal("LLM_API_KEY not set and /tmp/llm_key.txt not found")
	}
	log.Printf("Key loaded (%d chars) | %s", len(llmCfg.APIKey), llmCfg.Model)

	if len(os.Args) > 1 {
		pairCode = os.Args[1]
	}

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 10 * time.Second,
	}

	relayAddr := "wss://8.153.192.3:8099/mcp"
	if len(os.Args) > 2 {
		relayAddr = os.Args[2]
	}

	conn, _, err := dialer.Dial(relayAddr, nil)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	log.Printf("Connected to %s", relayAddr)

	// MCP initialize
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "hermes-agent", "version": "0.0.16"},
		},
	})
	var resp map[string]interface{}
	conn.ReadJSON(&resp)

	// Generate or reuse pairing code
	if pairCode == "" {
		conn.WriteJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]interface{}{"name": "voice_connect"},
		})
		conn.ReadJSON(&resp)
		content := resp["result"].(map[string]interface{})["content"].([]interface{})
		text := content[0].(map[string]interface{})["text"].(string)
		fmt.Sscanf(text, "Pairing code: %s", &pairCode)
		log.Printf("Pairing code: %s", pairCode)
	}

	log.Printf("Agent ready. Code: %s | LLM: %s", pairCode, llmCfg.Model)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	readCh := make(chan map[string]interface{}, 16)

	go func() {
		for {
			var msg map[string]interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("Read error: %v", err)
				close(readCh)
				return
			}
			readCh <- msg
		}
	}()

	for {
		select {
		case msg, ok := <-readCh:
			if !ok {
				log.Println("Connection closed")
				return
			}
			handleMessage(conn, msg)

		case <-interrupt:
			log.Println("Shutting down...")
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
			return
		}
	}
}

func readKey(envName, filePath string) string {
	if k := os.Getenv(envName); k != "" {
		return k
	}
	data, err := os.ReadFile(filePath)
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func handleMessage(conn *websocket.Conn, msg map[string]interface{}) {
	method, _ := msg["method"].(string)

	switch method {
	case "notifications/apk_chat":
		params, _ := msg["params"].(map[string]interface{})
		msgType, _ := params["type"].(string)
		content, _ := params["content"].(string)
		log.Printf("📱 APK: [%s] %s", msgType, content)

		if msgType == "chat" && content != "" {
			reply, err := callLLM(content)
			if err != nil {
				log.Printf("LLM error: %v", err)
				reply = "抱歉，我暂时无法回复。"
			}
			log.Printf("🤖 LLM: %s", truncate(reply, 80))
			sendVoiceChat(conn, reply)
		}

	default:
		if id, ok := msg["id"]; ok && id != nil {
			log.Printf("Request: method=%s id=%v", method, id)
		}
	}
}

func sendVoiceChat(conn *websocket.Conn, text string) {
	callResp := map[string]interface{}{
		"jsonrpc": "2.0", "id": time.Now().UnixMilli(),
		"method": "tools/call",
		"params": map[string]interface{}{
			"name": "voice_chat",
			"arguments": map[string]interface{}{
				"content": text,
			},
		},
	}
	if err := conn.WriteJSON(callResp); err != nil {
		log.Printf("Write voice_chat error: %v", err)
		return
	}
	var ack map[string]interface{}
	conn.ReadJSON(&ack)
	log.Printf("✅ Sent")
}

func callLLM(prompt string) (string, error) {
	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}

	reqBody := map[string]interface{}{
		"model":       llmCfg.Model,
		"messages":    messages,
		"max_tokens":  300,
		"temperature": 0.3,
		"stream":      false,
	}

	body, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequest("POST", llmCfg.APIBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+llmCfg.APIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
