// echonet-exporter is a Prometheus exporter for ECHONET Lite devices (EP Cube, solar, AC, etc.).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/styygeli/echonet-exporter/internal/config"
	"github.com/styygeli/echonet-exporter/internal/logging"
	"github.com/styygeli/echonet-exporter/internal/metrics"
	"github.com/styygeli/echonet-exporter/internal/scraper"
	"github.com/styygeli/echonet-exporter/internal/specs"
)

func main() {
	log := logging.New("main")
	logging.SetLevelFromEnv()

	if err := godotenv.Load(); err != nil {
		log.Infof("No .env file found, relying on existing environment variables.")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	deviceSpecs, err := specs.Load()
	if err != nil {
		log.Fatalf("load device specs: %v", err)
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
		log.Infof("Shutting down...")
		cancel()
		if err := server.Shutdown(context.Background()); err != nil {
			log.Errorf("HTTP shutdown: %v", err)
		}
	}()

	log.Infof("Listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}
}
