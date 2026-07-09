package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtraVarsContainsNoToken(t *testing.T) {
	c := validBase()
	vars := extraVars(c)
	data, err := json.Marshal(vars)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), c.DOToken) {
		t.Fatal("DO token must not appear in the extra-vars file")
	}
	// every interactive prompt in algo's input.yml must be answered
	for _, key := range []string{
		"provider", "server_name", "region", "users",
		"wireguard_enabled", "ipsec_enabled",
		"ondemand_cellular", "ondemand_wifi",
		"dns_adblocking", "ssh_tunneling", "store_pki",
	} {
		if _, ok := vars[key]; !ok {
			t.Errorf("extra vars missing %q", key)
		}
	}
	if vars["ipsec_enabled"] != false || vars["wireguard_enabled"] != true {
		t.Error("MVP protocol selection wrong")
	}
	if vars["store_pki"] != true {
		t.Error("store_pki must be true (unprivileged container cannot mount tmpfs)")
	}
}

func TestDeployStatePersistence(t *testing.T) {
	dir := t.TempDir()
	origState, origFile := StateDir, DeploysFile
	StateDir = dir
	DeploysFile = filepath.Join(dir, "deploys.json")
	defer func() { StateDir, DeploysFile = origState, origFile }()

	m := &deployManager{jobs: []DeployJob{{
		ID:        "20260101-000000",
		State:     StateRunning,
		StartedAt: time.Now(),
	}}}
	m.mtx.Lock()
	m.saveLocked()
	m.mtx.Unlock()

	// a fresh manager (plugin restart) must mark the running job interrupted
	m2 := &deployManager{}
	m2.load()
	job, ok := m2.current()
	if !ok {
		t.Fatal("expected a job after reload")
	}
	if job.State != StateInterrupted {
		t.Fatalf("expected interrupted, got %s", job.State)
	}

	st, err := os.Stat(DeploysFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0600 {
		t.Fatalf("deploys.json must be 0600, got %o", st.Mode().Perm())
	}
}

func TestStartRefusesWhenRunning(t *testing.T) {
	dir := t.TempDir()
	origState, origFile := StateDir, DeploysFile
	StateDir = dir
	DeploysFile = filepath.Join(dir, "deploys.json")
	defer func() { StateDir, DeploysFile = origState, origFile }()

	m := &deployManager{jobs: []DeployJob{{ID: "x", State: StateRunning}}}
	if _, err := m.start(validBase()); err == nil {
		t.Fatal("expected refusal while a deploy is running")
	}
}

func TestLogTailScrubs(t *testing.T) {
	dir := t.TempDir()
	token := "dop_v1_" + strings.Repeat("cd", 32)
	logFile := filepath.Join(dir, "deploy.log")
	content := "line1\nauth with " + token + "\nline3\n"
	if err := os.WriteFile(logFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	job := DeployJob{LogFile: logFile}
	tail := logTail(job, token, 1024)
	if strings.Contains(tail, token) {
		t.Fatal("token leaked in log tail")
	}
	if !strings.Contains(tail, "line3") {
		t.Fatal("expected log content in tail")
	}
	// tail smaller than file
	tail = logTail(job, token, 6)
	if len(tail) == 0 || len(tail) > 6+len(RedactedToken) {
		t.Fatalf("unexpected tail size %d", len(tail))
	}
}

func TestVPNConfigListingAndPaths(t *testing.T) {
	dir := t.TempDir()
	orig := AlgoConfigsDir
	AlgoConfigsDir = dir
	defer func() { AlgoConfigsDir = orig }()

	wg := filepath.Join(dir, "203.0.113.7", "wireguard")
	if err := os.MkdirAll(wg, 0700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"phone.conf", "phone.conf.png", "laptop.conf"} {
		if err := os.WriteFile(filepath.Join(wg, f), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// junk that must not be listed
	os.MkdirAll(filepath.Join(dir, "not a server", "wireguard"), 0700)
	os.WriteFile(filepath.Join(wg, "evil..conf"), []byte("x"), 0600)

	configs := listVPNConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configs, got %+v", configs)
	}
	if configs[0].User != "laptop" || configs[1].User != "phone" {
		t.Fatalf("unexpected order: %+v", configs)
	}
	if !configs[1].HasQR || configs[0].HasQR {
		t.Fatalf("QR detection wrong: %+v", configs)
	}

	// download path validation
	if p := vpnConfigPath("203.0.113.7", "phone.conf"); p == "" {
		t.Fatal("valid path rejected")
	}
	if p := vpnConfigPath("203.0.113.7", "phone.conf.png"); p == "" {
		t.Fatal("valid QR path rejected")
	}
	for _, bad := range [][2]string{
		{"../etc", "phone.conf"},
		{"203.0.113.7", "../algo.pem"},
		{"203.0.113.7", "phone.txt"},
		{"203.0.113.7", ".conf"},
		{"203.0.113.7", "pho ne.conf"},
		{"203.0.113.7", "phone.conf.png.png"},
		{"a..b", "phone.conf"},
	} {
		if p := vpnConfigPath(bad[0], bad[1]); p != "" {
			t.Errorf("accepted bad request %v -> %s", bad, p)
		}
	}
}
