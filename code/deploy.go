package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Deploy job states
const (
	StateRunning     = "running"
	StateSuccess     = "success"
	StateFailed      = "failed"
	StateInterrupted = "interrupted" // plugin restarted while a job was running
)

// DeployJob is one ansible-playbook run. Persisted (without log content) in
// DeploysFile so history and a running job survive UI reloads and restarts.
type DeployJob struct {
	ID         string
	State      string
	StartedAt  time.Time
	FinishedAt time.Time `json:",omitempty"`
	ExitCode   int
	Provider   string
	ServerName string
	Region     string
	Users      []string
	LogFile    string
	Error      string `json:",omitempty"`
}

type deployManager struct {
	mtx  sync.Mutex
	jobs []DeployJob
}

var gDeploys = &deployManager{}

// extraVars is the non-secret variable set handed to ansible with -e @file.
// The DigitalOcean token is passed via the DO_API_TOKEN environment variable
// (algo reads it there), keeping it off argv and out of the vars file.
func extraVars(c Config) map[string]interface{} {
	return map[string]interface{}{
		"provider":    "digitalocean",
		"server_name": c.ServerName,
		"region":      c.Region,
		"users":       c.Users,
		// MVP: WireGuard only
		"wireguard_enabled": true,
		"ipsec_enabled":     false,
		// answers for every interactive prompt in algo's input.yml
		"ondemand_cellular": c.OndemandCellular,
		"ondemand_wifi":     c.OndemandWifi,
		"dns_adblocking":    c.DNSAdblocking,
		"ssh_tunneling":     c.SSHTunneling,
		// keep the PKI so users can be added to the same server later; also
		// avoids algo's tmpfs mount path, which needs privileges we don't have
		"store_pki": true,
	}
}

func (m *deployManager) load() {
	data, err := os.ReadFile(DeploysFile)
	if err != nil {
		return
	}
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if err := json.Unmarshal(data, &m.jobs); err != nil {
		fmt.Println("[-] Failed to parse deploy state:", err)
		return
	}
	// The ansible child dies with us: anything still "running" after a
	// restart was interrupted.
	for i := range m.jobs {
		if m.jobs[i].State == StateRunning {
			m.jobs[i].State = StateInterrupted
			m.jobs[i].FinishedAt = time.Now()
			m.jobs[i].Error = "plugin restarted during deploy"
		}
	}
	m.saveLocked()
}

func (m *deployManager) saveLocked() {
	data, err := json.MarshalIndent(m.jobs, "", " ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(StateDir, 0700); err != nil {
		fmt.Println("[-] Failed to create state dir:", err)
		return
	}
	tmp := DeploysFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		fmt.Println("[-] Failed to write deploy state:", err)
		return
	}
	if err := os.Rename(tmp, DeploysFile); err != nil {
		fmt.Println("[-] Failed to write deploy state:", err)
	}
}

func (m *deployManager) current() (DeployJob, bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if len(m.jobs) == 0 {
		return DeployJob{}, false
	}
	return m.jobs[len(m.jobs)-1], true
}

func (m *deployManager) history() []DeployJob {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	out := make([]DeployJob, len(m.jobs))
	copy(out, m.jobs)
	return out
}

func (m *deployManager) running() bool {
	job, ok := m.current()
	return ok && job.State == StateRunning
}

// start launches ansible-playbook as a child process and returns the new job.
// Refuses if a job is already running.
func (m *deployManager) start(c Config) (DeployJob, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if n := len(m.jobs); n > 0 && m.jobs[n-1].State == StateRunning {
		return DeployJob{}, fmt.Errorf("a deploy is already running")
	}

	id := time.Now().UTC().Format("20060102-150405")
	job := DeployJob{
		ID:         id,
		State:      StateRunning,
		StartedAt:  time.Now(),
		Provider:   c.Provider,
		ServerName: c.ServerName,
		Region:     c.Region,
		Users:      append([]string{}, c.Users...),
		LogFile:    statePath("deploy-" + id + ".log"),
	}

	if err := os.MkdirAll(StateDir, 0700); err != nil {
		return DeployJob{}, err
	}

	varsData, err := json.MarshalIndent(extraVars(c), "", " ")
	if err != nil {
		return DeployJob{}, err
	}
	varsFile := statePath("deploy-" + id + "-vars.json")
	if err := os.WriteFile(varsFile, varsData, 0600); err != nil {
		return DeployJob{}, err
	}

	logf, err := os.OpenFile(job.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return DeployJob{}, err
	}

	cmd := exec.Command(VenvBin+"/ansible-playbook", "main.yml", "-e", "@"+varsFile)
	cmd.Dir = AlgoDir
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Env = append(os.Environ(),
		"PATH="+VenvBin+":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"DO_API_TOKEN="+c.DOToken,
		"ANSIBLE_NOCOLOR=1",
		"ANSIBLE_FORCE_COLOR=0",
		"PYTHONUNBUFFERED=1",
	)

	if err := cmd.Start(); err != nil {
		logf.Close()
		return DeployJob{}, err
	}

	m.jobs = append(m.jobs, job)
	m.saveLocked()

	go m.wait(job.ID, cmd, logf, varsFile)
	return job, nil
}

func (m *deployManager) wait(id string, cmd *exec.Cmd, logf *os.File, varsFile string) {
	err := cmd.Wait()
	logf.Close()
	os.Remove(varsFile)

	m.mtx.Lock()
	defer m.mtx.Unlock()
	for i := range m.jobs {
		if m.jobs[i].ID != id {
			continue
		}
		m.jobs[i].FinishedAt = time.Now()
		if err == nil {
			m.jobs[i].State = StateSuccess
			m.jobs[i].ExitCode = 0
		} else {
			m.jobs[i].State = StateFailed
			m.jobs[i].Error = err.Error()
			if exitErr, ok := err.(*exec.ExitError); ok {
				m.jobs[i].ExitCode = exitErr.ExitCode()
			} else {
				m.jobs[i].ExitCode = -1
			}
		}
		break
	}
	m.saveLocked()
}

// logTail returns up to maxBytes from the end of a job's log, secret-scrubbed.
func logTail(job DeployJob, token string, maxBytes int64) string {
	f, err := os.Open(job.LogFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := int64(0)
	if st.Size() > maxBytes {
		off = st.Size() - maxBytes
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return ""
	}
	return scrubSecrets(string(buf), token)
}
