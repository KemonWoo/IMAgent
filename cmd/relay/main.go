// IMAgent Relay — MCP server for Agent-to-APK voice, text, and file communication.
// V3: P2P mesh for AI community. V4: Self-evolution (metrics, self-healing, auto-update).
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
	"github.com/KemonWoo/IMAgent/internal/update"
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

	// V4: Metrics (Prometheus format)
	mux.HandleFunc("/metrics", relay.HandleMetrics)

	// V4: Version info
	mux.HandleFunc("/version", update.HandleVersion)

	// V4: Update check
	checker := update.NewChecker("KemonWoo", "IMAgent")
	mux.HandleFunc("/update/check", update.HandleUpdateCheck(checker))

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

		if *peers != "" {
			bootstrapAddrs := strings.Split(*peers, ",")
			relay.BootstrapPeers(bootstrapAddrs)
		}
	}

	// V4: Start self-healing health checker
	relay.StartHealthCheck()

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
		relay.StopHealthCheck()
		relay.Stop()
		server.Close()
	}()

	log.Printf("IMAgent Relay %s listening on %s", update.Version, addr)
	log.Printf("  Agent MCP endpoint: ws://0.0.0.0:%d/mcp", *port)
	log.Printf("  APK endpoint:        ws://0.0.0.0:%d/apk", *port)
	log.Printf("  Health check:        http://0.0.0.0:%d/health", *port)
	log.Printf("  Metrics:             http://0.0.0.0:%d/metrics", *port)
	log.Printf("  Version:             http://0.0.0.0:%d/version", *port)
	log.Printf("  File hosting:        %s", *wwwDir)
	log.Printf("  Upload storage:      %s", *uploadsDir)

	if p2pNode != "" {
		log.Printf("  P2P mesh:            enabled (node=%s, addr=%s)", p2pNode, p2pAddress)
		if *peers != "" {
			log.Printf("  Bootstrap peers:     %s", *peers)
		}
	}

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
