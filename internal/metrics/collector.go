package metrics

import (
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/echonet"
	"github.com/sty/echonet-exporter/internal/specs"
)

const namespace = "echonet"

// Collector implements prometheus.Collector and polls ECHONET devices on each scrape.
type Collector struct {
	cfg    *config.Config
	client *echonet.Client
	specs  map[string]*specs.DeviceSpec
	mutex  sync.Mutex

	scrapeSuccess       *prometheus.Desc
	scrapeDuration      *prometheus.Desc
	lastScrapeTimestamp *prometheus.Desc
	metricDescs         map[string]map[string]*prometheus.Desc // class -> metric name -> desc
}

// NewCollector returns a new collector.
func NewCollector(cfg *config.Config, deviceSpecs map[string]*specs.DeviceSpec) *Collector {
	c := &Collector{
		cfg:    cfg,
		client: echonet.NewClient(cfg.ScrapeTimeoutSec, deviceSpecs),
		specs:  deviceSpecs,
		scrapeSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_success"),
			"1 if the last scrape of this device succeeded, 0 otherwise.",
			[]string{"device", "ip", "class"},
			nil,
		),
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
			"Duration of the last scrape for this device in seconds.",
			[]string{"device", "ip", "class"},
			nil,
		),
		lastScrapeTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "last_scrape_timestamp_seconds"),
			"Unix timestamp of the last successful scrape for this device.",
			[]string{"device", "ip", "class"},
			nil,
		),
		metricDescs: make(map[string]map[string]*prometheus.Desc),
	}

	// Build metric descriptors from specs
	for class, spec := range deviceSpecs {
		if spec == nil {
			continue
		}
		c.metricDescs[class] = make(map[string]*prometheus.Desc)
		for _, m := range spec.Metrics {
			subsystem := class
			if class == "storage_battery" {
				subsystem = "battery"
			} else if class == "home_solar" {
				subsystem = "solar"
			} else if class == "home_ac" {
				subsystem = "ac"
			}
			c.metricDescs[class][m.Name] = prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsystem, m.Name),
				m.Help,
				[]string{"device", "ip", "class"},
				nil,
			)
		}
	}

	return c
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.scrapeSuccess
	ch <- c.scrapeDuration
	ch <- c.lastScrapeTimestamp
	for _, descs := range c.metricDescs {
		for _, d := range descs {
			ch <- d
		}
	}
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, dev := range c.cfg.Devices {
		result, err := c.client.ScrapeDevice(dev)
		success := 0.0
		if err == nil && result.Success {
			success = 1
		} else if err != nil {
			log.Printf("scrape %s (%s): %v", dev.Name, dev.IP, err)
		}

		ch <- prometheus.MustNewConstMetric(c.scrapeSuccess, prometheus.GaugeValue, success, dev.Name, dev.IP, dev.Class)
		ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, result.DurationSec, dev.Name, dev.IP, dev.Class)

		if result.Success {
			ch <- prometheus.MustNewConstMetric(c.lastScrapeTimestamp, prometheus.GaugeValue, float64(time.Now().Unix()), dev.Name, dev.IP, dev.Class)
		}

		for name, mv := range result.Metrics {
			desc, ok := c.metricDescs[dev.Class][name]
			if !ok {
				continue
			}
			vt := prometheus.GaugeValue
			if mv.Type == "counter" {
				vt = prometheus.CounterValue
			}
			ch <- prometheus.MustNewConstMetric(desc, vt, mv.Value, dev.Name, dev.IP, dev.Class)
		}
	}
}
