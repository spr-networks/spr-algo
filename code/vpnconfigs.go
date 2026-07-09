package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// VPNConfig is one generated per-user WireGuard profile on a deployed server.
type VPNConfig struct {
	Server string // server identifier = algo output dir name (the instance IP)
	User   string
	File   string // download path: /configs/<Server>/<User>.conf
	HasQR  bool   // algo also rendered <User>.conf.png
}

// algo names output dirs after the instance IP (v4 or v6)
var reServerDir = regexp.MustCompile(`^[0-9a-fA-F.:]{3,45}$`)

// listVPNConfigs walks algo's output tree: <AlgoConfigsDir>/<ip>/wireguard/*.conf
func listVPNConfigs() []VPNConfig {
	out := []VPNConfig{}
	entries, err := os.ReadDir(AlgoConfigsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || !reServerDir.MatchString(e.Name()) {
			continue
		}
		wgDir := filepath.Join(AlgoConfigsDir, e.Name(), "wireguard")
		files, err := os.ReadDir(wgDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !strings.HasSuffix(name, ".conf") {
				continue
			}
			user := strings.TrimSuffix(name, ".conf")
			if !reUser.MatchString(user) {
				continue
			}
			_, qrErr := os.Stat(filepath.Join(wgDir, name+".png"))
			out = append(out, VPNConfig{
				Server: e.Name(),
				User:   user,
				File:   "/configs/" + e.Name() + "/" + user + ".conf",
				HasQR:  qrErr == nil,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Server != out[j].Server {
			return out[i].Server < out[j].Server
		}
		return out[i].User < out[j].User
	})
	return out
}

// vpnConfigPath validates a download request and maps it onto the output
// tree. Returns "" if the request does not name a valid server/user pair.
// filename must be <user>.conf or <user>.conf.png.
func vpnConfigPath(server, filename string) string {
	if !reServerDir.MatchString(server) || strings.Contains(server, "..") {
		return ""
	}
	png := strings.HasSuffix(filename, ".conf.png")
	user := strings.TrimSuffix(strings.TrimSuffix(filename, ".png"), ".conf")
	if (!png && !strings.HasSuffix(filename, ".conf")) || !reUser.MatchString(user) {
		return ""
	}
	return filepath.Join(AlgoConfigsDir, server, "wireguard", filename)
}
