package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RouterStatus struct {
	Uptime       string  `json:"uptime"`
	UptimeDays   int     `json:"uptime_days"`
	Load         string  `json:"load"`
	Load1        float64 `json:"load_1"`
	Load5        float64 `json:"load_5"`
	Memory       string  `json:"memory"`
	MemUsedMB    int     `json:"mem_used_mb"`
	MemTotalMB   int     `json:"mem_total_mb"`
	MemPercent   float64 `json:"mem_percent"`
	Status       string  `json:"status"`
	LastCheck    string  `json:"last_check"`
	ErrorMessage string  `json:"error,omitempty"`
}

type Config struct {
	ListenAddr   string
	RouterHost   string
	SSHUser      string
	SSHKey       string
	SSHPort      int
	ProxyTarget  string
	CacheTTLSec  int
}

var (
	cfg         Config
	cacheMu     sync.RWMutex
	cachedStats *RouterStatus
	lastFetch   time.Time
)

func fetchRouterStats() (*RouterStatus, error) {
	// Command to execute on router via SSH
	remoteCmd := "cat /proc/uptime; echo '---'; cat /proc/loadavg; echo '---'; free"

	args := []string{
		"-p", strconv.Itoa(cfg.SSHPort),
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
	}

	if cfg.SSHKey != "" {
		args = append(args, "-i", cfg.SSHKey)
	}

	target := fmt.Sprintf("%s@%s", cfg.SSHUser, cfg.RouterHost)
	args = append(args, target, remoteCmd)

	cmd := exec.Command("ssh", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ssh execution failed: %w", err)
	}

	parts := strings.Split(string(output), "---")
	if len(parts) < 3 {
		return nil, fmt.Errorf("unexpected output structure from router")
	}

	status := &RouterStatus{
		Status:    "Online",
		LastCheck: time.Now().Format("2006-01-02 15:04:05"),
	}

	// 1. Parse Uptime
	uptimeFields := strings.Fields(parts[0])
	if len(uptimeFields) > 0 {
		if sec, err := strconv.ParseFloat(uptimeFields[0], 64); err == nil {
			days := int(sec / 86400)
			status.UptimeDays = days
			status.Uptime = fmt.Sprintf("%d天", days)
		}
	}

	// 2. Parse Load
	loadFields := strings.Fields(parts[1])
	if len(loadFields) >= 2 {
		status.Load = loadFields[0]
		status.Load1, _ = strconv.ParseFloat(loadFields[0], 64)
		status.Load5, _ = strconv.ParseFloat(loadFields[1], 64)
	}

	// 3. Parse Free/Memory
	memLines := strings.Split(strings.TrimSpace(parts[2]), "\n")
	for _, line := range memLines {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasPrefix(fields[0], "Mem:") {
			totalKB, _ := strconv.Atoi(fields[1])
			usedKB, _ := strconv.Atoi(fields[2])
			if totalKB > 0 {
				status.MemTotalMB = totalKB / 1024
				status.MemUsedMB = usedKB / 1024
				status.MemPercent = float64(int(float64(usedKB)/float64(totalKB)*1000)) / 10.0
				status.Memory = fmt.Sprintf("%.1f%% (%dMB)", status.MemPercent, status.MemUsedMB)
			}
			break
		}
	}

	return status, nil
}

func getStatus() *RouterStatus {
	cacheMu.RLock()
	if cachedStats != nil && time.Since(lastFetch) < time.Duration(cfg.CacheTTLSec)*time.Second {
		defer cacheMu.RUnlock()
		return cachedStats
	}
	cacheMu.RUnlock()

	cacheMu.Lock()
	defer cacheMu.Unlock()

	if cachedStats != nil && time.Since(lastFetch) < time.Duration(cfg.CacheTTLSec)*time.Second {
		return cachedStats
	}

	stats, err := fetchRouterStats()
	if err != nil {
		log.Printf("[Probe] Error updating router stats: %v", err)
		if cachedStats != nil {
			return cachedStats
		}
		return &RouterStatus{
			Status:       "Offline",
			ErrorMessage: err.Error(),
			LastCheck:    time.Now().Format("2006-01-02 15:04:05"),
		}
	}

	cachedStats = stats
	lastFetch = time.Now()
	return cachedStats
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	status := getStatus()
	_ = json.NewEncoder(w).Encode(status)
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	if cfg.ProxyTarget == "" {
		http.NotFound(w, r)
		return
	}

	targetURL := fmt.Sprintf("%s%s", strings.TrimRight(cfg.ProxyTarget, "/"), r.URL.RequestURI())
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	for k, vv := range r.Header {
		if strings.EqualFold(k, "Host") || strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Proxy error: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func main() {
	flag.StringVar(&cfg.ListenAddr, "listen", ":18080", "HTTP listen address")
	flag.StringVar(&cfg.RouterHost, "router", "192.168.1.1", "Router IP or hostname")
	flag.StringVar(&cfg.SSHUser, "user", "admin", "SSH user on router")
	flag.StringVar(&cfg.SSHKey, "key", "", "Path to private key for SSH auth")
	flag.IntVar(&cfg.SSHPort, "port", 22, "SSH port on router")
	flag.StringVar(&cfg.ProxyTarget, "proxy", "http://192.168.1.1:80", "Target URL to reverse proxy (e.g. router web UI)")
	flag.IntVar(&cfg.CacheTTLSec, "cache", 10, "Cache TTL for status metrics in seconds")
	flag.Parse()

	log.Printf("Starting Pandavan Homepage Probe on %s", cfg.ListenAddr)
	log.Printf("Target router: %s@%s:%d (proxy target: %s)", cfg.SSHUser, cfg.RouterHost, cfg.SSHPort, cfg.ProxyTarget)

	http.HandleFunc("/api/status.json", handleStatus)
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status.json" || r.URL.Path == "/status" {
			handleStatus(w, r)
			return
		}
		handleProxy(w, r)
	})

	if err := http.ListenAndServe(cfg.ListenAddr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
