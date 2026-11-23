package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type EnvInfo struct {
	RunID           string            `json:"run_id"`
	Timestamp       time.Time         `json:"timestamp"`
	GitSHA          string            `json:"git_sha"`
	GitBranch       string            `json:"git_branch"`
	GoVersion       string            `json:"go_version"`
	OS              string            `json:"os"`
	Arch            string            `json:"arch"`
	NumCPU          int               `json:"num_cpu"`
	FeatureFlags    map[string]string `json:"feature_flags"`
	ContainerImages map[string]string `json:"container_images"`
	ResourceLimits  map[string]string `json:"resource_limits"`
}

func main() {
	runID := flag.String("run-id", "", "Benchmark Run identifier (e.g. run-20261123-01)")
	outPath := flag.String("out", "", "Output path for env.json (defaults to results/<run-id>/env.json)")
	flag.Parse()

	if *runID == "" {
		*runID = fmt.Sprintf("run-%s", time.Now().Format("20060102-150405"))
	}

	destination := *outPath
	if destination == "" {
		destination = filepath.Join("results", *runID, "env.json")
	}

	gitSHA := runCmd("git", "rev-parse", "HEAD")
	gitBranch := runCmd("git", "branch", "--show-current")

	info := EnvInfo{
		RunID:     *runID,
		Timestamp: time.Now().UTC(),
		GitSHA:    gitSHA,
		GitBranch: gitBranch,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		NumCPU:    runtime.NumCPU(),
		FeatureFlags: map[string]string{
			"ORDERFLOW_CACHE_ENABLED":      envOrDefault("ORDERFLOW_CACHE_ENABLED", "true"),
			"ORDERFLOW_SOLDOUT_FASTPATH":   envOrDefault("ORDERFLOW_SOLDOUT_FASTPATH", "true"),
			"ORDERFLOW_RATE_LIMIT_ENABLED": envOrDefault("ORDERFLOW_RATE_LIMIT_ENABLED", "true"),
			"LOAD_TEST_MODE":               envOrDefault("LOAD_TEST_MODE", "false"),
		},
		ContainerImages: map[string]string{
			"postgres": "postgres:17-alpine",
			"redis":    "redis:7.4-alpine",
			"redpanda": "docker.redpanda.com/redpandadata/redpanda:v24.3.6",
		},
		ResourceLimits: map[string]string{
			"api":      "1.0 CPU, 512MB RAM",
			"worker":   "1.0 CPU, 512MB RAM",
			"postgres": "1.5 CPU, 1024MB RAM",
			"redis":    "1.0 CPU, 512MB RAM",
			"redpanda": "1.0 CPU, 1024MB RAM",
		},
	}

	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory %s: %v\n", dir, err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal env info: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(destination, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", destination, err)
		os.Exit(1)
	}

	fmt.Printf("Captured benchmark environment to %s\n", destination)
}

func envOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func runCmd(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "unknown"
	}
	return strings.TrimSpace(out.String())
}
