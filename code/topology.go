package main

import (
	"net/http"
	"sort"
)

// Topology mirrors the SPR plugin topology contract (see spr-tailscale): the
// host fetches GET /topology and merges the plugin graph into the router
// topology view at the "root" anchor node.

type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

// isTerminal reports whether a deploy job has finished (one way or another).
func isTerminal(state string) bool {
	return state == StateSuccess || state == StateFailed || state == StateInterrupted
}

// buildTopology derives the plugin's contribution to the SPR topology from
// deploy history and the generated per-user profiles.
//
//   - Always: the root anchor {ID:"root", ConnType:"wireguard", Online:true}.
//   - After at least one successful deploy: one "vpn-server" node per algo
//     output directory (named after the instance IP), plus one "profile" node
//     per generated user profile. Profiles are credentials, not live devices,
//     so they are Kind "profile" and always Online=false.
//   - Edges run toward root: profile -> server -> root, Layer "vpn".
//   - Server Online reflects whether the most recently *finished* deploy
//     succeeded (a running deploy does not change it).
func buildTopology(jobs []DeployJob, configs []VPNConfig) Topology {
	topo := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: "wireguard", Online: true}},
		Edges: []TopoEdge{},
	}

	// Most recent successful deploy names the server; most recent finished
	// deploy decides whether it is online.
	var lastSuccess *DeployJob
	online := false
	for i := len(jobs) - 1; i >= 0; i-- {
		if jobs[i].State == StateSuccess {
			lastSuccess = &jobs[i]
			break
		}
	}
	if lastSuccess == nil {
		return topo // never deployed successfully: root only
	}
	for i := len(jobs) - 1; i >= 0; i-- {
		if isTerminal(jobs[i].State) {
			online = jobs[i].State == StateSuccess
			break
		}
	}

	serverLabel := lastSuccess.ServerName
	if lastSuccess.Region != "" {
		serverLabel += " (" + lastSuccess.Region + ")"
	}

	// One server node per algo output dir (dir name = instance IP), with its
	// profiles attached. A successful deploy with no profiles on disk still
	// yields a server node (without an IP) so the graph reflects reality.
	servers := map[string]string{} // ip -> node ID
	for _, cfg := range configs {
		if _, ok := servers[cfg.Server]; !ok {
			id := "server:" + cfg.Server
			servers[cfg.Server] = id
			topo.Nodes = append(topo.Nodes, TopoNode{
				ID:       id,
				Kind:     "vpn-server",
				Name:     serverLabel,
				IP:       cfg.Server,
				ConnType: "wireguard",
				Online:   online,
			})
			topo.Edges = append(topo.Edges, TopoEdge{
				From: id, To: "root", Layer: "vpn", Kind: "wireguard",
			})
		}
		profileID := "profile:" + cfg.Server + ":" + cfg.User
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:     profileID,
			Kind:   "profile",
			Name:   cfg.User,
			Online: false,
		})
		topo.Edges = append(topo.Edges, TopoEdge{
			From: profileID, To: servers[cfg.Server], Layer: "vpn", Kind: "wireguard",
		})
	}
	if len(servers) == 0 {
		id := "server:" + lastSuccess.ServerName
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:       id,
			Kind:     "vpn-server",
			Name:     serverLabel,
			ConnType: "wireguard",
			Online:   online,
		})
		topo.Edges = append(topo.Edges, TopoEdge{
			From: id, To: "root", Layer: "vpn", Kind: "wireguard",
		})
	}

	sort.Slice(topo.Nodes, func(i, j int) bool { return topo.Nodes[i].ID < topo.Nodes[j].ID })
	sort.Slice(topo.Edges, func(i, j int) bool {
		if topo.Edges[i].From != topo.Edges[j].From {
			return topo.Edges[i].From < topo.Edges[j].From
		}
		return topo.Edges[i].To < topo.Edges[j].To
	})
	return topo
}

// GET /topology — plugin contribution to the SPR topology view
func handleTopology(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, buildTopology(gDeploys.history(), listVPNConfigs()))
}
