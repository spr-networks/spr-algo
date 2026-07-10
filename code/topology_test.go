package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func job(id, state, server, region string) DeployJob {
	return DeployJob{
		ID:         id,
		State:      state,
		StartedAt:  time.Now(),
		ServerName: server,
		Region:     region,
	}
}

func findNode(t *testing.T, topo Topology, id string) TopoNode {
	t.Helper()
	for _, n := range topo.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in %+v", id, topo.Nodes)
	return TopoNode{}
}

func hasEdge(topo Topology, from, to string) bool {
	for _, e := range topo.Edges {
		if e.From == from && e.To == to && e.Layer == "vpn" && e.Kind == "wireguard" {
			return true
		}
	}
	return false
}

func TestTopologyRootOnlyWhenNeverDeployed(t *testing.T) {
	for _, jobs := range [][]DeployJob{
		nil,
		{job("1", StateFailed, "algo", "nyc3")},
		{job("1", StateRunning, "algo", "nyc3")},
		{job("1", StateInterrupted, "algo", "nyc3")},
	} {
		topo := buildTopology(jobs, []VPNConfig{{Server: "203.0.113.7", User: "phone"}})
		if len(topo.Nodes) != 1 || len(topo.Edges) != 0 {
			t.Fatalf("expected root-only graph for jobs %+v, got %+v", jobs, topo)
		}
		root := topo.Nodes[0]
		if root.ID != "root" || root.ConnType != "wireguard" || !root.Online {
			t.Fatalf("bad root anchor: %+v", root)
		}
	}
}

func TestTopologyAfterSuccessfulDeploy(t *testing.T) {
	jobs := []DeployJob{
		job("1", StateFailed, "algo", "nyc3"),
		job("2", StateSuccess, "algo", "nyc3"),
	}
	configs := []VPNConfig{
		{Server: "203.0.113.7", User: "laptop", HasQR: false},
		{Server: "203.0.113.7", User: "phone", HasQR: true},
	}
	topo := buildTopology(jobs, configs)

	if len(topo.Nodes) != 4 { // root + server + 2 profiles
		t.Fatalf("expected 4 nodes, got %+v", topo.Nodes)
	}
	srv := findNode(t, topo, "server:203.0.113.7")
	if srv.Kind != "vpn-server" || srv.Name != "algo (nyc3)" ||
		srv.IP != "203.0.113.7" || srv.ConnType != "wireguard" || !srv.Online {
		t.Fatalf("bad server node: %+v", srv)
	}
	for _, user := range []string{"phone", "laptop"} {
		p := findNode(t, topo, "profile:203.0.113.7:"+user)
		if p.Kind != "profile" || p.Name != user || p.Online || p.IP != "" {
			t.Fatalf("bad profile node: %+v", p)
		}
		if !hasEdge(topo, p.ID, srv.ID) {
			t.Fatalf("missing edge %s -> %s", p.ID, srv.ID)
		}
	}
	if !hasEdge(topo, srv.ID, "root") {
		t.Fatal("missing server -> root edge")
	}
	if len(topo.Edges) != 3 {
		t.Fatalf("expected 3 edges, got %+v", topo.Edges)
	}
}

func TestTopologyServerOfflineAfterFailedRedeploy(t *testing.T) {
	jobs := []DeployJob{
		job("1", StateSuccess, "algo", "ams3"),
		job("2", StateFailed, "algo", "ams3"),
	}
	topo := buildTopology(jobs, []VPNConfig{{Server: "203.0.113.7", User: "phone"}})
	srv := findNode(t, topo, "server:203.0.113.7")
	if srv.Online {
		t.Fatal("server must be offline when the last finished deploy failed")
	}
	if srv.Name != "algo (ams3)" {
		t.Fatalf("server should be named from the last successful deploy: %+v", srv)
	}
}

func TestTopologyRunningDeployKeepsLastResult(t *testing.T) {
	jobs := []DeployJob{
		job("1", StateSuccess, "algo", "nyc3"),
		job("2", StateRunning, "algo", "nyc3"),
	}
	topo := buildTopology(jobs, []VPNConfig{{Server: "203.0.113.7", User: "phone"}})
	if srv := findNode(t, topo, "server:203.0.113.7"); !srv.Online {
		t.Fatal("a running deploy must not mark the server offline")
	}
}

func TestTopologySuccessWithoutProfilesStillShowsServer(t *testing.T) {
	jobs := []DeployJob{job("1", StateSuccess, "myvpn", "sgp1")}
	topo := buildTopology(jobs, nil)
	srv := findNode(t, topo, "server:myvpn")
	if srv.Kind != "vpn-server" || srv.IP != "" || srv.Name != "myvpn (sgp1)" {
		t.Fatalf("bad IP-less server node: %+v", srv)
	}
	if !hasEdge(topo, srv.ID, "root") || len(topo.Edges) != 1 {
		t.Fatalf("expected only server -> root edge, got %+v", topo.Edges)
	}
}

func TestTopologyMultipleServers(t *testing.T) {
	jobs := []DeployJob{job("1", StateSuccess, "algo", "nyc3")}
	configs := []VPNConfig{
		{Server: "203.0.113.7", User: "phone"},
		{Server: "198.51.100.4", User: "phone"},
	}
	topo := buildTopology(jobs, configs)
	if len(topo.Nodes) != 5 || len(topo.Edges) != 4 {
		t.Fatalf("expected 2 servers + 2 profiles + root, got %+v", topo)
	}
	findNode(t, topo, "server:203.0.113.7")
	findNode(t, topo, "server:198.51.100.4")
	if !hasEdge(topo, "profile:198.51.100.4:phone", "server:198.51.100.4") {
		t.Fatal("profile attached to wrong server")
	}
}

// The graph builder must work directly off the persisted deploy-state format
// (deploys.json) — build from a raw fixture as loaded from disk.
func TestTopologyFromDeployStateFixture(t *testing.T) {
	fixture := `[
	 {"ID":"20260101-101500","State":"failed","StartedAt":"2026-01-01T10:15:00Z",
	  "FinishedAt":"2026-01-01T10:16:40Z","ExitCode":2,"Provider":"digitalocean",
	  "ServerName":"algo","Region":"nyc3","Users":["phone"],"LogFile":"","Error":"exit status 2"},
	 {"ID":"20260101-120000","State":"success","StartedAt":"2026-01-01T12:00:00Z",
	  "FinishedAt":"2026-01-01T12:08:12Z","ExitCode":0,"Provider":"digitalocean",
	  "ServerName":"algo","Region":"nyc3","Users":["phone","laptop"],"LogFile":""}
	]`
	var jobs []DeployJob
	if err := json.Unmarshal([]byte(fixture), &jobs); err != nil {
		t.Fatal(err)
	}
	topo := buildTopology(jobs, []VPNConfig{
		{Server: "203.0.113.7", User: "laptop"},
		{Server: "203.0.113.7", User: "phone"},
	})
	if len(topo.Nodes) != 4 || len(topo.Edges) != 3 {
		t.Fatalf("unexpected graph: %+v", topo)
	}
	if srv := findNode(t, topo, "server:203.0.113.7"); !srv.Online || srv.Name != "algo (nyc3)" {
		t.Fatalf("bad server node from fixture: %+v", srv)
	}

	// wire-format check: root anchor omits IP, profiles omit ConnType
	data, err := json.Marshal(topo)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `{"ID":"root","Kind":"","Name":"","ConnType":"wireguard","Online":true}`) {
		t.Fatalf("root anchor wire format wrong: %s", s)
	}
}
