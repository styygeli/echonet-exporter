package scraper

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/echonet"
	"github.com/sty/echonet-exporter/internal/specs"
)

// Cache holds the latest scraped metrics per device. Safe for concurrent use.
type Cache struct {
	mu      sync.RWMutex
	metrics map[string]deviceCache // deviceKey -> metrics + scrape state
}

type deviceCache struct {
	groups  map[string]groupStatus
	metrics map[string]echonet.MetricValue
}

type groupStatus struct {
	interval    time.Duration
	success     bool
	durationSec float64
	lastAttempt time.Time
	lastSuccess time.Time
}

// deviceKey returns a unique key for a configured device.
func deviceKey(dev config.Device) string {
	return dev.Name + "|" + dev.IP + "|" + dev.Class
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{metrics: make(map[string]deviceCache)}
}

// Get returns aggregated scrape status and a copy of cached metrics for a device.
func (c *Cache) Get(dev config.Device) (success bool, durationSec float64, lastScrape time.Time, metrics map[string]echonet.MetricValue) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	dc, ok := c.metrics[deviceKey(dev)]
	if !ok {
		return false, 0, time.Time{}, nil
	}

	now := time.Now()
	latestAttempt := time.Time{}
	latestSuccess := time.Time{}
	latestDuration := 0.0
	aggregatedSuccess := false

	for _, gs := range dc.groups {
		if gs.lastAttempt.After(latestAttempt) {
			latestAttempt = gs.lastAttempt
			latestDuration = gs.durationSec
		}
		if gs.lastSuccess.After(latestSuccess) {
			latestSuccess = gs.lastSuccess
		}
		if gs.success {
			// Consider success valid within a bounded freshness window.
			ttl := gs.interval * 2
			if ttl < 5*time.Second {
				ttl = 5 * time.Second
			}
			if now.Sub(gs.lastAttempt) <= ttl {
				aggregatedSuccess = true
			}
		}
	}

	mcopy := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		mcopy[k] = v
	}
	return aggregatedSuccess, latestDuration, latestSuccess, mcopy
}

// Update merges a scrape result into the cache for a device/group.
func (c *Cache) Update(dev config.Device, groupID string, interval time.Duration, success bool, durationSec float64, metrics map[string]echonet.MetricValue) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := deviceKey(dev)
	dc := c.metrics[key]
	if dc.groups == nil {
		dc.groups = make(map[string]groupStatus)
	}
	if dc.metrics == nil {
		dc.metrics = make(map[string]echonet.MetricValue)
	}

	now := time.Now()
	gs := dc.groups[groupID]
	gs.interval = interval
	gs.success = success
	gs.durationSec = durationSec
	gs.lastAttempt = now
	if success {
		gs.lastSuccess = now
		for k, v := range metrics {
			dc.metrics[k] = v
		}
	}
	dc.groups[groupID] = gs

	c.metrics[key] = dc
}

// Start begins background scrapers for all configured devices. Call with a context
// that is cancelled on shutdown.
func (c *Cache) Start(ctx context.Context, cfg *config.Config, deviceSpecs map[string]*specs.DeviceSpec) {
	client := echonet.NewClient(cfg.ScrapeTimeoutSec)

	for _, dev := range cfg.Devices {
		spec, ok := deviceSpecs[dev.Class]
		if !ok || spec == nil {
			log.Printf("scraper: unknown class %q for device %s, skipping", dev.Class, dev.Name)
			continue
		}

		// Per-device interval override from config
		devDefaultInterval := spec.DefaultScrapeInterval
		if dev.ScrapeInterval != "" {
			d, err := time.ParseDuration(dev.ScrapeInterval)
			if err != nil {
				log.Printf("scraper: device %s invalid scrape_interval %q: %v", dev.Name, dev.ScrapeInterval, err)
			} else if d > 0 {
				devDefaultInterval = d
			}
		}

		// Group metrics by their scrape interval
		byInterval := make(map[time.Duration][]specs.MetricSpec)
		for _, m := range spec.Metrics {
			iv := m.ScrapeInterval
			if iv <= 0 {
				iv = devDefaultInterval
			}
			byInterval[iv] = append(byInterval[iv], m)
		}

		intervals := make([]time.Duration, 0, len(byInterval))
		for iv := range byInterval {
			intervals = append(intervals, iv)
		}
		sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })

		for i, interval := range intervals {
			metrics := byInterval[interval]
			groupID := interval.String()

			// Stagger startup scrapes to avoid request bursts on low-power devices.
			initialDelay := time.Duration(i) * 500 * time.Millisecond
			if initialDelay > interval/2 {
				initialDelay = interval / 2
			}

			go c.runScraper(ctx, client, dev, spec, metrics, groupID, interval, initialDelay)
		}
	}
}

func (c *Cache) runScraper(ctx context.Context, client *echonet.Client, dev config.Device, spec *specs.DeviceSpec, metrics []specs.MetricSpec, groupID string, interval, initialDelay time.Duration) {
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(initialDelay):
		}
	}

	// Scrape once immediately after optional startup delay.
	c.scrapeOnce(client, dev, spec, metrics, groupID, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scrapeOnce(client, dev, spec, metrics, groupID, interval)
		}
	}
}

func (c *Cache) scrapeOnce(client *echonet.Client, dev config.Device, spec *specs.DeviceSpec, metrics []specs.MetricSpec, groupID string, interval time.Duration) {
	epcs := make([]byte, 0, len(metrics))
	for _, m := range metrics {
		epcs = append(epcs, m.EPC)
	}

	start := time.Now()
	raw, err := client.SendGet(dev.IP, spec.EOJ, epcs)
	durationSec := time.Since(start).Seconds()
	if err != nil {
		log.Printf("scrape %s (%s): %v", dev.Name, dev.IP, err)
		c.Update(dev, groupID, interval, false, durationSec, nil)
		return
	}

	_, props, err := echonet.ParseGetRes(raw)
	if err != nil {
		log.Printf("scrape %s (%s): parse: %v", dev.Name, dev.IP, err)
		c.Update(dev, groupID, interval, false, durationSec, nil)
		return
	}

	out := echonet.ParsePropsToMetrics(props, metrics)
	c.Update(dev, groupID, interval, true, durationSec, out)
}
