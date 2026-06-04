// IMAgent Relay — MCP server for Agent-to-APK voice, text, and file communication.
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
	flag.Parse()

	relay := transport.NewRelay(*uploadsDir)

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

	// File upload
	mux.HandleFunc("/upload", relay.HandleUpload)

	// File download — merges wwwDir and uploadsDir
	os.MkdirAll(*wwwDir, 0755)
	os.MkdirAll(*uploadsDir, 0755)
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		// Try wwwDir first, then uploadsDir
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
		server.Close()
	}()

	log.Printf("IMAgent Relay listening on %s", addr)
	log.Printf("  Agent MCP endpoint: ws://0.0.0.0:%d/mcp", *port)
	log.Printf("  APK endpoint:        ws://0.0.0.0:%d/apk", *port)
	log.Printf("  Health check:        http://0.0.0.0:%d/health", *port)
	log.Printf("  File hosting:        %s", *wwwDir)
	log.Printf("  Upload storage:      %s", *uploadsDir)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
