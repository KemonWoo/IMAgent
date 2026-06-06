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

var (
	pairCode  string
	apiKey    string
	apiBase   = "https://api.deepseek.com/v1"
	apiModel  = "deepseek-v4-pro"
)

func main() {
	// Read API key from file or env
	apiKey = os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		data, err := os.ReadFile("/tmp/ds_key.txt")
		if err == nil {
			apiKey = strings.TrimSpace(string(data))
		}
	}
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY not set and /tmp/ds_key.txt not found")
	}
	log.Printf("API key loaded (%d chars)", len(apiKey))

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
			"clientInfo":      map[string]string{"name": "hermes-agent", "version": "0.0.13"},
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

	log.Printf("Agent ready. Code: %s | LLM: %s", pairCode, apiModel)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Channel to serialize reads (gorilla/websocket is not concurrent-safe for reads)
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

func handleMessage(conn *websocket.Conn, msg map[string]interface{}) {
	method, _ := msg["method"].(string)

	switch method {
	case "notifications/apk_chat":
		params, _ := msg["params"].(map[string]interface{})
		msgType, _ := params["type"].(string)
		content, _ := params["content"].(string)
		log.Printf("📱 APK: [%s] %s", msgType, content)

		if msgType == "chat" && content != "" {
			// Call DeepSeek
			reply, err := callDeepSeek(content)
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
	// Read ack
	var ack map[string]interface{}
	conn.ReadJSON(&ack)
	log.Printf("✅ Sent")
}

func callDeepSeek(prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model": apiModel,
		"messages": []map[string]string{
			{"role": "system", "content": "你是知微，一个IMAgent AI助手。简洁友好地回复用户。用中文。"},
			{"role": "user", "content": prompt},
		},
		"max_tokens":   500,
		"temperature":  0.7,
		"stream":       false,
	}

	body, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequest("POST", apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

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
