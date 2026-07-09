package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Paths are variables so tests can point them at a temp dir.
var (
	ConfigDir  = "/configs/spr-algo"
	ConfigFile = "/configs/spr-algo/config.json"
	// algo's output tree; /algo/configs is a symlink to this (see startup.sh)
	AlgoConfigsDir = "/configs/spr-algo/algo"
	AlgoDir        = "/algo"
	AlgoCommitFile = "/algo/.algo-commit"
	StateDir       = "/state/plugins/spr-algo"
	DeploysFile    = "/state/plugins/spr-algo/deploys.json"
	VenvBin        = "/opt/algo-venv/bin"
)

const RedactedToken = "***"

// Config is persisted at ConfigFile (0600). DOToken is write-only: it is
// stored on disk but never returned by GET /config (see redacted()).
type Config struct {
	Provider   string // MVP: "digitalocean"
	DOToken    string `json:",omitempty"`
	Region     string
	ServerName string
	Users      []string

	// MVP: WireGuard on, IPsec off, on-demand flags off. Kept in the config
	// for forward compatibility; PUT /config forces these values.
	WireGuardEnabled bool
	IPsecEnabled     bool
	OndemandCellular bool
	OndemandWifi     bool
	DNSAdblocking    bool
	SSHTunneling     bool
}

// ConfigResponse is what GET /config returns: the config with the token
// replaced by a configured flag.
type ConfigResponse struct {
	Config
	DOTokenConfigured bool
}

var (
	configMtx sync.RWMutex
	gConfig   = defaultConfig()
)

func defaultConfig() Config {
	return Config{
		Provider:         "digitalocean",
		ServerName:       "algo",
		Users:            []string{},
		WireGuardEnabled: true,
	}
}

var (
	// algo user names become filenames, wireguard peer names and (with
	// ssh_tunneling) unix accounts. Strict allow-list, no dots (they would
	// allow "user.conf"-style ambiguity in download paths).
	reUser = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	// DigitalOcean slugs like nyc3, ams3, sfo3
	reRegion = regexp.MustCompile(`^[a-z]{3}[0-9]{1,2}$`)
	// droplet / server name; algo also sanitizes but be strict up front
	reServerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
	// DO tokens: legacy 64-hex or dop_v1_<64 hex>; allow a superset of safe chars
	reDOToken = regexp.MustCompile(`^[A-Za-z0-9_]{32,256}$`)
	// scrub any DO-style token that might get echoed into logs
	reTokenScrub = regexp.MustCompile(`dop_v1_[0-9a-f]{64}|\b[0-9a-f]{64}\b`)
)

func validateConfig(c *Config) error {
	if c.Provider != "digitalocean" {
		return fmt.Errorf("unsupported provider %q (MVP supports: digitalocean)", c.Provider)
	}
	if c.ServerName == "" {
		c.ServerName = "algo"
	}
	if !reServerName.MatchString(c.ServerName) {
		return fmt.Errorf("invalid server name (allowed: letters, digits, dashes, max 32)")
	}
	if c.Region != "" && !reRegion.MatchString(c.Region) {
		return fmt.Errorf("invalid region slug %q", c.Region)
	}
	if c.DOToken != "" && c.DOToken != RedactedToken && !reDOToken.MatchString(c.DOToken) {
		return fmt.Errorf("invalid DigitalOcean token format")
	}
	if len(c.Users) > 250 {
		return fmt.Errorf("too many users")
	}
	seen := map[string]bool{}
	for _, u := range c.Users {
		if !reUser.MatchString(u) {
			return fmt.Errorf("invalid user name %q (allowed: letters, digits, _ and -, max 32)", u)
		}
		if seen[u] {
			return fmt.Errorf("duplicate user %q", u)
		}
		seen[u] = true
	}
	// enforce MVP invariants
	c.WireGuardEnabled = true
	c.IPsecEnabled = false
	c.OndemandCellular = false
	c.OndemandWifi = false
	c.DNSAdblocking = false
	c.SSHTunneling = false
	return nil
}

func (c Config) redacted() ConfigResponse {
	resp := ConfigResponse{Config: c, DOTokenConfigured: c.DOToken != ""}
	resp.DOToken = ""
	if resp.Users == nil {
		resp.Users = []string{}
	}
	return resp
}

// readyToDeploy reports why a deploy cannot start, or nil.
func (c Config) readyToDeploy() error {
	if c.DOToken == "" {
		return fmt.Errorf("DigitalOcean API token is not configured")
	}
	if c.Region == "" {
		return fmt.Errorf("region is not configured")
	}
	if len(c.Users) == 0 {
		return fmt.Errorf("at least one user is required")
	}
	return nil
}

func loadConfig() {
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		return // first run
	}
	c := defaultConfig()
	if err := json.Unmarshal(data, &c); err != nil {
		fmt.Println("[-] Failed to parse config:", err)
		return
	}
	configMtx.Lock()
	defer configMtx.Unlock()
	gConfig = c
}

func writeConfigLocked() error {
	data, err := json.MarshalIndent(gConfig, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(ConfigDir, 0700); err != nil {
		return err
	}
	tmp := ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, ConfigFile)
}

// scrubSecrets removes the configured token and anything token-shaped from
// text destined for API responses.
func scrubSecrets(s string, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, RedactedToken)
	}
	return reTokenScrub.ReplaceAllString(s, RedactedToken)
}

func algoCommit() string {
	data, err := os.ReadFile(AlgoCommitFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func statePath(elem ...string) string {
	return filepath.Join(append([]string{StateDir}, elem...)...)
}
