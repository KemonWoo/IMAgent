//go:build ignore

// IMAgent persistent bridge: maintains MCP WebSocket connection to relay,
// listens for APK messages, and echoes them for testing.
package main

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

var relayURL = "wss://8.153.192.3:8099/mcp"

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime)

	dialer := websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 10 * time.Second,
	}

	for {
		log.Println("Connecting to relay...")
		conn, _, err := dialer.Dial(relayURL, nil)
		if err != nil {
			log.Printf("Dial failed: %v, retrying in 5s", err)
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("Connected!")

		// MCP initialize
		conn.WriteJSON(map[string]interface{}{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]string{"name": "hermes-agent", "version": "1.0"},
			},
		})
		var initResp map[string]interface{}
		if err := conn.ReadJSON(&initResp); err != nil {
			log.Printf("Init read: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		log.Println("MCP initialized")

		// Main read loop - receives messages from relay (APK messages, etc.)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Read error: %v", err)
				break
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(raw, &msg); err != nil {
				log.Printf("JSON parse error: %v", err)
				continue
			}

			msgType, _ := msg["type"].(string)
			msgFrom, _ := msg["from"].(string)
			msgContent, _ := msg["content"].(string)

			log.Printf("RECV: type=%s from=%s content=%s", msgType, msgFrom, truncate(msgContent, 100))
			log.Printf("FULL: %s", string(raw))
		}

		log.Println("Connection lost, reconnecting in 3s...")
		conn.Close()
		time.Sleep(3 * time.Second)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
