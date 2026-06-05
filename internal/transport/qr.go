package transport

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	qrcode "github.com/skip2/go-qrcode"
)

// qrCache stores generated QR PNGs indexed by code.
var qrCache = struct {
	sync.RWMutex
	m map[string][]byte
}{m: make(map[string][]byte)}

// SetPublicAddr sets the relay's public address for QR code generation.
func (r *Relay) SetPublicAddr(addr string) {
	r.publicAddr = addr
}

// GetPublicAddr returns the relay's public address.
func (r *Relay) GetPublicAddr() string {
	if r.publicAddr != "" {
		return r.publicAddr
	}
	return fmt.Sprintf("localhost:%d", 8099) // fallback
}

// StoreQR generates a QR PNG for the given code and caches it.
// Returns the URL path for the QR image.
func (r *Relay) StoreQR(code string) string {
	addr := r.GetPublicAddr()
	uri := fmt.Sprintf("imagent://pair?r=%s&c=%s", addr, code)

	png, err := qrcode.Encode(uri, qrcode.Medium, 320)
	if err != nil {
		log.Printf("QR encode error: %v", err)
		return ""
	}

	qrCache.Lock()
	qrCache.m[code] = png
	qrCache.Unlock()

	// Also return a base64 data URI for use in web panel
	return fmt.Sprintf("/qr/%s", code)
}

// GetQRBase64 returns the QR as a base64 data URI for embedding in HTML.
func (r *Relay) GetQRBase64(code string) string {
	qrCache.RLock()
	png, ok := qrCache.m[code]
	qrCache.RUnlock()

	if !ok {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// HandleQR serves a cached QR code PNG.
func (r *Relay) HandleQR(w http.ResponseWriter, req *http.Request) {
	code := strings.TrimPrefix(req.URL.Path, "/qr/")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	qrCache.RLock()
	png, ok := qrCache.m[code]
	qrCache.RUnlock()

	if !ok {
		http.Error(w, "QR not found or expired", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(png)
}

// BuildRelayURI builds the imagent:// pairing URI from relay address and code.
func BuildRelayURI(relayAddr, code string) string {
	return fmt.Sprintf("imagent://pair?r=%s&c=%s", relayAddr, code)
}
