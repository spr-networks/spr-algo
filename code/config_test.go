package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func validBase() Config {
	return Config{
		Provider:   "digitalocean",
		DOToken:    "dop_v1_" + strings.Repeat("ab", 32),
		Region:     "nyc3",
		ServerName: "algo",
		Users:      []string{"phone", "laptop"},
	}
}

func TestValidateConfigOK(t *testing.T) {
	c := validBase()
	if err := validateConfig(&c); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	// MVP invariants must be forced regardless of input
	c = validBase()
	c.IPsecEnabled = true
	c.OndemandWifi = true
	c.WireGuardEnabled = false
	if err := validateConfig(&c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.IPsecEnabled || c.OndemandWifi || !c.WireGuardEnabled {
		t.Fatalf("MVP invariants not enforced: %+v", c)
	}
}

func TestValidateConfigRejects(t *testing.T) {
	cases := []func(*Config){
		func(c *Config) { c.Provider = "aws" },
		func(c *Config) { c.Provider = "" },
		func(c *Config) { c.Region = "nyc3; rm -rf /" },
		func(c *Config) { c.Region = "NYC3" },
		func(c *Config) { c.ServerName = "my server" },
		func(c *Config) { c.ServerName = "a$b" },
		func(c *Config) { c.Users = []string{"bob", "bob"} },
		func(c *Config) { c.Users = []string{"../etc/passwd"} },
		func(c *Config) { c.Users = []string{"user.name"} },
		func(c *Config) { c.Users = []string{"user name"} },
		func(c *Config) { c.Users = []string{""} },
		func(c *Config) { c.Users = []string{strings.Repeat("a", 33)} },
		func(c *Config) { c.DOToken = "short" },
		func(c *Config) { c.DOToken = "bad token with spaces and $(cmd)" },
	}
	for i, mutate := range cases {
		c := validBase()
		mutate(&c)
		if err := validateConfig(&c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestValidateConfigDefaultsServerName(t *testing.T) {
	c := validBase()
	c.ServerName = ""
	if err := validateConfig(&c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.ServerName != "algo" {
		t.Fatalf("expected default server name, got %q", c.ServerName)
	}
}

func TestRedactedNeverEchoesToken(t *testing.T) {
	c := validBase()
	resp := c.redacted()
	if resp.DOToken != "" {
		t.Fatalf("token leaked in redacted config: %q", resp.DOToken)
	}
	if !resp.DOTokenConfigured {
		t.Fatal("expected DOTokenConfigured=true")
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), c.DOToken) {
		t.Fatal("token leaked in serialized response")
	}
}

func TestReadyToDeploy(t *testing.T) {
	c := validBase()
	if err := c.readyToDeploy(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	for i, mutate := range []func(*Config){
		func(c *Config) { c.DOToken = "" },
		func(c *Config) { c.Region = "" },
		func(c *Config) { c.Users = nil },
	} {
		c := validBase()
		mutate(&c)
		if err := c.readyToDeploy(); err == nil {
			t.Errorf("case %d: expected not ready", i)
		}
	}
}

func TestScrubSecrets(t *testing.T) {
	token := "dop_v1_" + strings.Repeat("42", 32)
	log := "TASK [x] using " + token + " done\nplain hex " + strings.Repeat("ab", 32) + " end\nsafe line\n"
	out := scrubSecrets(log, token)
	if strings.Contains(out, token) {
		t.Fatal("configured token not scrubbed")
	}
	if strings.Contains(out, strings.Repeat("ab", 32)) {
		t.Fatal("token-shaped hex not scrubbed")
	}
	if !strings.Contains(out, "safe line") {
		t.Fatal("scrubbing destroyed unrelated content")
	}
	// scrubbing with an empty configured token must not panic
	_ = scrubSecrets(log, "")
}
