// IMAgent Relay — MCP server for Agent-to-APK voice and text communication.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/KemonWoo/IMAgent/internal/transport"
)

func main() {
	port := flag.Int("port", 8099, "HTTP/WebSocket listen port")
	wwwDir := flag.String("www", "/var/www/html", "Static file serving directory (APK hosting)")
	flag.Parse()

	relay := transport.NewRelay()

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

	// File upload (V2 placeholder)
	mux.HandleFunc("/upload", relay.HandleUpload)

	// File download (APK hosting) — configurable directory
	os.MkdirAll(*wwwDir, 0755)
	mux.Handle("/dl/", http.StripPrefix("/dl/", http.FileServer(http.Dir(*wwwDir))))

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

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}
