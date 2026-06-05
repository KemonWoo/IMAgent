// IMAgent Relay — MCP server for Agent-to-APK voice, text, and file communication.
// V3: P2P mesh for AI community. V4: Self-evolution. V2: TLS encryption.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/KemonWoo/IMAgent/internal/transport"
	"github.com/KemonWoo/IMAgent/internal/update"
)

func main() {
	port := flag.Int("port", 8099, "HTTP/WebSocket listen port")
	wwwDir := flag.String("www", "/var/www/html", "Static file serving directory (APK hosting)")
	uploadsDir := flag.String("uploads", filepath.Join(os.TempDir(), "imagent-uploads"), "File upload storage directory")

	// V2: TLS encryption
	tlsCert := flag.String("tls-cert", "", "TLS certificate file (PEM). Enables HTTPS/wss when set.")
	tlsKey := flag.String("tls-key", "", "TLS private key file (PEM). Required with --tls-cert.")
	autoCert := flag.String("auto-cert", "", "Auto-generate self-signed cert+key to this directory. Prompts user to trust on first connect.")

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

	// File download
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

	// --- TLS setup ---
	certFile := *tlsCert
	keyFile := *tlsKey
	useTLS := certFile != "" && keyFile != ""

	// Auto-generate self-signed cert if --auto-cert is set and no explicit cert
	if *autoCert != "" && !useTLS {
		os.MkdirAll(*autoCert, 0700)
		certFile = filepath.Join(*autoCert, "cert.pem")
		keyFile = filepath.Join(*autoCert, "key.pem")
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			if err := generateSelfSigned(certFile, keyFile); err != nil {
				log.Fatalf("Auto-cert generation failed: %v", err)
			}
			log.Printf("TLS: auto-generated self-signed cert in %s", *autoCert)
		}
		useTLS = true
	}

	// Build scheme strings for logging
	scheme := "http"
	wsScheme := "ws"
	p2pScheme := "http"
	if useTLS {
		scheme = "https"
		wsScheme = "wss"
		p2pScheme = "https"
		// Set TLS config for P2P HTTP client
		relay.SetUseTLS(true)
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
		relay.StopHealthCheck()
		relay.Stop()
		server.Close()
	}()

	log.Printf("IMAgent Relay %s listening on %s (%s)", update.Version, addr, scheme)
	log.Printf("  Agent MCP endpoint: %s://0.0.0.0:%d/mcp", wsScheme, *port)
	log.Printf("  APK endpoint:        %s://0.0.0.0:%d/apk", wsScheme, *port)
	log.Printf("  Health check:        %s://0.0.0.0:%d/health", scheme, *port)
	log.Printf("  Metrics:             %s://0.0.0.0:%d/metrics", scheme, *port)
	log.Printf("  Version:             %s://0.0.0.0:%d/version", scheme, *port)
	log.Printf("  File hosting:        %s", *wwwDir)
	log.Printf("  Upload storage:      %s", *uploadsDir)

	if p2pNode != "" {
		log.Printf("  P2P mesh:            enabled (node=%s, addr=%s://%s)", p2pNode, p2pScheme, p2pAddress)
		if *peers != "" {
			log.Printf("  Bootstrap peers:     %s", *peers)
		}
	}

	var serveErr error
	if useTLS {
		serveErr = server.ListenAndServeTLS(certFile, keyFile)
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != http.ErrServerClosed {
		log.Fatalf("Server error: %v", serveErr)
	}
	log.Println("Server stopped")
}

// generateSelfSigned creates a self-signed TLS certificate valid for 1 year.
func generateSelfSigned(certFile, keyFile string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("keygen: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "IMAgent Relay",
			Organization: []string{"IMAgent Self-Signed"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           nil,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("cert create: %w", err)
	}

	// Write cert
	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	// Write key
	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes, _ := x509.MarshalECPrivateKey(priv)
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	os.Chmod(keyFile, 0600)

	return nil
}
