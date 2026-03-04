// echonet-exporter is a Prometheus exporter for ECHONET Lite devices (EP Cube, solar, AC, etc.).
package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/metrics"
	"github.com/sty/echonet-exporter/internal/specs"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on existing environment variables.")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config: %v", err)
	}

	deviceSpecs, err := specs.Load()
	if err != nil {
		log.Fatalf("Load device specs: %v", err)
	}

	collector := metrics.NewCollector(cfg, deviceSpecs)
	prometheus.MustRegister(collector)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>ECHONET Exporter</title></head>
<body><h1>ECHONET Lite Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`))
	})

	log.Printf("Listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, nil); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}
