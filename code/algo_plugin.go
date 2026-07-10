// spr-algo: SPR plugin that deploys a personal Algo VPN (WireGuard) server to
// a cloud provider using the vendored trailofbits/algo ansible playbooks.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var UNIX_PLUGIN_LISTENER = "/state/plugins/spr-algo/socket"

const logTailBytes = 16 * 1024

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		fmt.Println("[-] encode failed:", err)
	}
}

// GET /status — plugin overview for the UI status card
func handleStatus(w http.ResponseWriter, r *http.Request) {
	configMtx.RLock()
	c := gConfig
	configMtx.RUnlock()

	job, hasJob := gDeploys.current()
	status := map[string]interface{}{
		"AlgoCommit":        algoCommit(),
		"Provider":          c.Provider,
		"DOTokenConfigured": c.DOToken != "",
		"Region":            c.Region,
		"UsersCount":        len(c.Users),
		"Deploying":         hasJob && job.State == StateRunning,
		"VPNConfigs":        len(listVPNConfigs()),
	}
	if hasJob {
		status["LastDeployState"] = job.State
		status["LastDeployStartedAt"] = job.StartedAt
	}
	jsonOK(w, status)
}

// GET /config
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	configMtx.RLock()
	defer configMtx.RUnlock()
	jsonOK(w, gConfig.redacted())
}

// PUT /config — DOToken empty or "***" means "keep the stored token"
func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var in Config
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := validateConfig(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	configMtx.Lock()
	defer configMtx.Unlock()
	if in.DOToken == "" || in.DOToken == RedactedToken {
		in.DOToken = gConfig.DOToken
	}
	if in.Users == nil {
		in.Users = []string{}
	}
	gConfig = in
	if err := writeConfigLocked(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOK(w, gConfig.redacted())
}

// POST /users — replace the user list. Applying the change to a deployed
// server requires a new deploy (POST /deploy); documented in the README.
func handlePostUsers(w http.ResponseWriter, r *http.Request) {
	var in struct{ Users []string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if in.Users == nil {
		in.Users = []string{}
	}

	configMtx.Lock()
	defer configMtx.Unlock()
	c := gConfig
	c.Users = in.Users
	if err := validateConfig(&c); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	gConfig = c
	if err := writeConfigLocked(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonOK(w, gConfig.redacted())
}

// POST /deploy — start an ansible run in the background
func handleDeploy(w http.ResponseWriter, r *http.Request) {
	configMtx.RLock()
	c := gConfig
	configMtx.RUnlock()

	if err := c.readyToDeploy(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	job, err := gDeploys.start(c)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	jsonOK(w, job)
}

// GET /deploy/status — latest job + sanitized log tail
func handleDeployStatus(w http.ResponseWriter, r *http.Request) {
	configMtx.RLock()
	token := gConfig.DOToken
	configMtx.RUnlock()

	job, ok := gDeploys.current()
	if !ok {
		jsonOK(w, map[string]interface{}{"State": "none"})
		return
	}
	resp := struct {
		DeployJob
		LogTail string
	}{job, logTail(job, token, logTailBytes)}
	resp.LogFile = "" // internal path, not useful to the UI
	jsonOK(w, resp)
}

// GET /deploys — history without logs
func handleDeploys(w http.ResponseWriter, r *http.Request) {
	jobs := gDeploys.history()
	for i := range jobs {
		jobs[i].LogFile = ""
	}
	jsonOK(w, jobs)
}

// GET /configs — list generated per-user wireguard profiles
func handleListVPNConfigs(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, listVPNConfigs())
}

// GET /configs/{server}/{file} — download <user>.conf as text/plain, or
// <user>.conf.png (the QR code algo renders) as JSON {PNGBase64} so the
// plugin-ui API client (json/text only) can consume it.
func handleDownloadVPNConfig(w http.ResponseWriter, r *http.Request) {
	path := vpnConfigPath(r.PathValue("server"), r.PathValue("file"))
	if path == "" {
		http.Error(w, "not found", 404)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if filepath.Ext(path) == ".png" {
		jsonOK(w, map[string]string{
			"Name":      filepath.Base(path),
			"PNGBase64": base64.StdEncoding.EncodeToString(data),
		})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
	w.Write(data)
}

type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL.Path)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	loadConfig()
	gDeploys.load()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleStatus)
	mux.HandleFunc("GET /config", handleGetConfig)
	mux.HandleFunc("PUT /config", handlePutConfig)
	mux.HandleFunc("POST /users", handlePostUsers)
	mux.HandleFunc("POST /deploy", handleDeploy)
	mux.HandleFunc("GET /deploy/status", handleDeployStatus)
	mux.HandleFunc("GET /deploys", handleDeploys)
	mux.HandleFunc("GET /configs", handleListVPNConfigs)
	mux.HandleFunc("GET /topology", handleTopology)
	mux.HandleFunc("GET /configs/{server}/{file}", handleDownloadVPNConfig)

	// UI (index.html is fetched via the socket and shown as iframe srcDoc)
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})

	os.Remove(UNIX_PLUGIN_LISTENER)
	if err := os.MkdirAll(filepath.Dir(UNIX_PLUGIN_LISTENER), 0700); err != nil {
		panic(err)
	}
	listener, err := net.Listen("unix", UNIX_PLUGIN_LISTENER)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UNIX_PLUGIN_LISTENER, 0770); err != nil {
		panic(err)
	}

	server := http.Server{
		Handler:           logRequest(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Println("[+] spr-algo plugin listening on", UNIX_PLUGIN_LISTENER)
	if err := server.Serve(listener); err != nil {
		panic(err)
	}
}
