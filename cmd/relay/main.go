// IMAgent Relay — MCP server for Agent-to-APK voice, text, and file communication.
// V3: P2P mesh for AI community (node discovery, AI-to-AI chat, decentralized routing).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/KemonWoo/IMAgent/internal/transport"
)

func main() {
	port := flag.Int("port", 8099, "HTTP/WebSocket listen port")
	wwwDir := flag.String("www", "/var/www/html", "Static file serving directory (APK hosting)")
	uploadsDir := flag.String("uploads", filepath.Join(os.TempDir(), "imagent-uploads"), "File upload storage directory")

	// V3: P2P mesh
	p2pNodeID := flag.String("p2p-id", "", "Unique node ID for this relay in the mesh (auto-generated if empty)")
	p2pAddr := flag.String("p2p-addr", "", "Public address for this relay (host:port) for mesh communication")
	peers := flag.String("peers", "", "Comma-separated list of bootstrap peer addresses (host:port)")

	flag.Parse()

	// Auto-generate P2P node ID if P2P address is specified but no ID
	p2pNode := *p2pNodeID
	p2pAddress := *p2pAddr
	if p2pAddress != "" && p2pNode == "" {
		hostname, _ := os.Hostname()
		p2pNode = fmt.Sprintf("%s-%d", hostname, *port)
	}

	relay := transport.NewRelay(*uploadsDir, p2pNode, p2pAddress)

	mux := http.NewServeMux()

	// MCP Agent endpoint (WebSocket)
	mux.HandleFunc("/mcp", relay.HandleAgentWS)

	// APK endpoint (WebSocket)
	mux.HandleFunc("/apk", relay.HandleAPKWS)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	// Web panel
	mux.HandleFunc("/", relay.HandlePanelRoot)
	mux.HandleFunc("/panel/status", relay.HandlePanelStatus)
	mux.HandleFunc("/panel/reset", relay.HandlePanelReset)

	// File upload
	mux.HandleFunc("/upload", relay.HandleUpload)

	// File download — merges wwwDir and uploadsDir
	os.MkdirAll(*wwwDir, 0755)
	os.MkdirAll(*uploadsDir, 0755)
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/dl/")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		fullPath := filepath.Join(*wwwDir, path)
		if _, err := os.Stat(fullPath); err == nil {
			http.ServeFile(w, r, fullPath)
			return
		}
		fullPath = filepath.Join(*uploadsDir, path)
		if _, err := os.Stat(fullPath); err == nil {
			http.ServeFile(w, r, fullPath)
			return
		}
		http.NotFound(w, r)
	})

	// V3: P2P mesh endpoints
	if p2pNode != "" {
		mux.HandleFunc("/p2p/announce", relay.HandleP2PAnnounce)
		mux.HandleFunc("/p2p/peers", relay.HandleP2PPeers)
		mux.HandleFunc("/p2p/agents", relay.HandleP2PAgents)
		mux.HandleFunc("/p2p/sync", relay.HandleP2PSync)
		mux.HandleFunc("/p2p/forward", relay.HandleP2PForward)

		// Bootstrap to initial peers
		if *peers != "" {
			bootstrapAddrs := strings.Split(*peers, ",")
			relay.BootstrapPeers(bootstrapAddrs)
		}
	}

	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		relay.Stop()
		server.Close()
	}()

	log.Printf("IMAgent Relay V3 listening on %s", addr)
	log.Printf("  Agent MCP endpoint: ws://0.0.0.0:%d/mcp", *port)
	log.Printf("  APK endpoint:        ws://0.0.0.0:%d/apk", *port)
	log.Printf("  Health check:        http://0.0.0.0:%d/health", *port)
	log.Printf("  File hosting:        %s", *wwwDir)
	log.Printf("  Upload storage:      %s", *uploadsDir)

	if p2pNode != "" {
		log.Printf("  P2P mesh:            enabled (node=%s, addr=%s)", p2pNode, p2pAddress)
		if *peers != "" {
			log.Printf("  Bootstrap peers:     %s", *peers)
		}
		log.Printf("  Mesh endpoints:      /p2p/announce /p2p/peers /p2p/agents /p2p/forward")
	}

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
