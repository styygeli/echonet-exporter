// echonet-exporter is a Prometheus exporter for ECHONET Lite devices (EP Cube, solar, AC, etc.).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/metrics"
	"github.com/sty/echonet-exporter/internal/scraper"
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

	cache := scraper.NewCache()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.Start(ctx, cfg, deviceSpecs)

	collector := metrics.NewCollector(cfg, cache, deviceSpecs)
	prometheus.MustRegister(collector)

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>ECHONET Exporter</title></head>
<body><h1>ECHONET Lite Exporter</h1><p><a href="/metrics">Metrics</a></p></body></html>`))
	})

	server := &http.Server{Addr: cfg.ListenAddr}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down...")
		cancel()
		server.Shutdown(context.Background())
	}()

	log.Printf("Listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}
}
