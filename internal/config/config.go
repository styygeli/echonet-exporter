package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Config holds exporter configuration from environment.
type Config struct {
	ListenAddr        string
	ScrapeTimeoutSec  int
	Devices           []Device
}

// Device is a single ECHONET device to scrape.
type Device struct {
	Name   string            `json:"name"`
	IP     string            `json:"ip"`
	Class  string            `json:"class"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Load reads configuration from environment (and .env already loaded by main).
func Load() (*Config, error) {
	listenAddr := os.Getenv("ECHONET_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":9191"
	}
	timeoutSec := 15
	if s := os.Getenv("ECHONET_SCRAPE_TIMEOUT_SEC"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			timeoutSec = n
		}
	}

	devicesJSON := os.Getenv("ECHONET_DEVICES")
	if devicesJSON == "" {
		return &Config{
			ListenAddr:       listenAddr,
			ScrapeTimeoutSec: timeoutSec,
			Devices:          nil,
		}, nil
	}

	var devices []Device
	if err := json.Unmarshal([]byte(devicesJSON), &devices); err != nil {
		return nil, fmt.Errorf("ECHONET_DEVICES JSON: %w", err)
	}

	return &Config{
		ListenAddr:       listenAddr,
		ScrapeTimeoutSec: timeoutSec,
		Devices:          devices,
	}, nil
}
