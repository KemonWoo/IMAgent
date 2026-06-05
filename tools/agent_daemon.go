//go:build ignore

package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

var pairCode string

func main() {
	if len(os.Args) > 1 {
		pairCode = os.Args[1]
	}

	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
	initReq := map[string]interface{}{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]string{"name": "hermes-agent", "version": "0.0.12"},
		},
	}
	conn.WriteJSON(initReq)
	var resp map[string]interface{}
	conn.ReadJSON(&resp)
	log.Printf("Initialized: %v", resp["result"].(map[string]interface{})["serverInfo"])

	// Generate or reuse pairing code
	if pairCode == "" {
		conn.WriteJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": 2, "method": "tools/call",
			"params": map[string]interface{}{"name": "voice_connect"},
		})
		conn.ReadJSON(&resp)
		content := resp["result"].(map[string]interface{})["content"].([]interface{})
		text := content[0].(map[string]interface{})["text"].(string)
		log.Printf("Pairing: %s", text)

		// Extract code
		fmt.Sscanf(text, "Pairing code: %s", &pairCode)
	}

	log.Printf("Agent ready. Pairing code: %s", pairCode)

	// Signal handling
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var msg map[string]interface{}
			err := conn.ReadJSON(&msg)
			if err != nil {
				log.Printf("Read error: %v", err)
				return
			}

			method, _ := msg["method"].(string)
			switch method {
			case "notifications/apk_chat":
				params, _ := msg["params"].(map[string]interface{})
				msgType, _ := params["type"].(string)
				content, _ := params["content"].(string)
				log.Printf("📱 APK: [%s] %s", msgType, content)

				if msgType == "chat" && content != "" {
					// Respond using voice_chat
					response := fmt.Sprintf("收到你的消息：「%s」\n—— 知微 (Hermes Agent)", content)
					callResp := map[string]interface{}{
						"jsonrpc": "2.0", "id": time.Now().UnixMilli(),
						"method": "tools/call",
						"params": map[string]interface{}{
							"name": "voice_chat",
							"arguments": map[string]interface{}{
								"content": response,
							},
						},
					}
					if err := conn.WriteJSON(callResp); err != nil {
						log.Printf("Write voice_chat error: %v", err)
						return
					}
					// Read the voice_chat response (ack)
					var ack map[string]interface{}
					conn.ReadJSON(&ack)
					log.Printf("✅ Response sent")
				}

			default:
				if id, ok := msg["id"]; ok && id != nil {
					log.Printf("Received request: method=%s id=%v", method, id)
				}
			}
		}
	}()

	// Heartbeat
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			log.Println("Agent disconnected")
			return
		case <-interrupt:
			log.Println("Shutting down...")
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "agent shutting down"))
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
			return
		case <-ticker.C:
			log.Printf("💓 Heartbeat (pair_code=%s)", pairCode)
		}
	}
}
