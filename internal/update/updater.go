// Package update provides self-update and version checking for IMAgent Relay.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// Version is the current relay version (set at build time via ldflags).
var Version = "4.0.0"

// Info holds version and build metadata.
type Info struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	BuildTime string `json:"build_time,omitempty"`
}

// GetInfo returns version metadata.
func GetInfo() Info {
	return Info{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// ReleaseInfo from GitHub releases API.
type ReleaseInfo struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
}

// Checker queries GitHub for newer releases.
type Checker struct {
	repoOwner string // "KemonWoo"
	repoName  string // "IMAgent"
	client    *http.Client
}

// NewChecker creates a release checker.
func NewChecker(owner, name string) *Checker {
	return &Checker{
		repoOwner: owner,
		repoName:  name,
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

// CheckLatest fetches the latest release from GitHub.
func (c *Checker) CheckLatest() (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", c.repoOwner, c.repoName)
	resp, err := c.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API status %d", resp.StatusCode)
	}

	var rel ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}
	return &rel, nil
}

// IsNewer returns true if the release tag is newer than the current version.
func IsNewer(current, latest string) bool {
	// Strip 'v' prefix
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")
	return latest > current
}

// HandleVersion serves the /version endpoint.
func HandleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetInfo())
}

// HandleUpdateCheck serves the /update/check endpoint.
func HandleUpdateCheck(c *Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		latest, err := c.CheckLatest()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"current": Version,
				"error":   err.Error(),
			})
			return
		}

		newer := IsNewer(Version, latest.TagName)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"current":  Version,
			"latest":   latest.TagName,
			"newer":    newer,
			"url":      latest.HTMLURL,
			"notes":    latest.Body,
			"released": latest.PublishedAt,
		})
	}
}
