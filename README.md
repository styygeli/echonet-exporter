# ECHONET Lite Exporter

A Prometheus exporter for ECHONET Lite devices: EP Cube (battery and solar), home air conditioners (e.g. Daikin, Mitsubishi), and compatible gear. It polls configured devices over UDP and exposes metrics for scraping by VictoriaMetrics or Prometheus.

## Features

- **Detached Scraping**: Background goroutines poll devices at configurable intervals, while the `/metrics` endpoint serves cached values instantly. This prevents overloading low-power IoT devices (like ESP32-based hardware) during frequent Prometheus scrapes.
- **Zero-Code Extensibility**: Device specifications (EOJ, EPCs, data types) are defined in YAML files. You can add support for new devices without writing Go code.
- **Multi-Interval Batching**: Metrics with the same scrape interval are batched into a single UDP request to minimize network traffic.
- **Dynamic Labels**: Attach custom labels to your devices in the configuration to organize them in Prometheus/VictoriaMetrics.
- **Capability-Aware Polling**: On startup, the exporter reads each device `GETMAP` (`EPC 0x9F`) and automatically skips configured EPCs the device does not report as readable.
- **Adaptive OPC Fallback**: If a device returns partial responses for larger EPC batches, the exporter automatically retries with smaller split batches.

## Configuration

Configuration is via environment variables (and optionally a `.env` file).

| Variable | Description | Default |
|----------|-------------|---------|
| `ECHONET_LISTEN_ADDR` | HTTP listen address | `:9191` |
| `ECHONET_SCRAPE_TIMEOUT_SEC` | Timeout in seconds for each device scrape | `15` |
| `ECHONET_LOG_LEVEL` | Log verbosity (`debug`, `info`, `warn`, `error`) | `info` |
| `ECHONET_DEVICES` | JSON array of device definitions | (none) |

Each device in `ECHONET_DEVICES` must have:

- **name** – short identifier (used as `device` label).
- **ip** – device IP on the LAN.
- **class** – one of:
  - `storage_battery` – EP Cube / storage battery (SoC, power, cumulative charge/discharge).
  - `home_solar` – home solar power generation (instantaneous and cumulative).
  - `home_ac` – home air conditioner (operation status, indoor/set temp, mode).

Optional **labels** – map of extra label names and values.

Optional **scrape_interval** – per-device detached scrape interval override (e.g. `"2m"`).

### Example `.env`

```env
ECHONET_LISTEN_ADDR=:9191
ECHONET_SCRAPE_TIMEOUT_SEC=15
ECHONET_DEVICES='[
  {"name":"epcube_battery","ip":"192.168.1.10","class":"storage_battery","scrape_interval":"2m","labels":{"site":"home"}},
  {"name":"epcube_solar","ip":"192.168.1.10","class":"home_solar"},
  {"name":"living_ac","ip":"192.168.1.20","class":"home_ac"}
]'
```

See [.env.example](.env.example) for a copy-paste template.

### Logging

The exporter uses a centralized logging layer:

- `debug` and `info` logs go to **stdout**
- `warn` and `error` logs go to **stderr**

Use `ECHONET_LOG_LEVEL` to control verbosity:

- `debug` - detailed protocol diagnostics (TID mismatches, ignored frames)
- `info` - startup and lifecycle messages
- `warn` - malformed/partial client responses and degraded behavior
- `error` - scrape failures and hard problems

## Building and running

```bash
go build -o echonet-exporter ./cmd/echonet-exporter
./echonet-exporter
```

With a `.env` in the current directory, the exporter loads it automatically. Then open:

- **http://localhost:9191/** – landing page
- **http://localhost:9191/metrics** – Prometheus metrics

## Metrics

All metrics use the prefix `echonet_` and the labels `device`, `ip`, `class`, plus any custom `labels` defined in your configuration.

The specific metrics exposed depend on the YAML device specifications. The default built-in specifications provide:

### Scrape health

- `echonet_scrape_success` – 1 if the last scrape succeeded, 0 otherwise.
- `echonet_scrape_duration_seconds` – duration of the last scrape.
- `echonet_last_scrape_timestamp_seconds` – Unix time of the last successful scrape.
- `echonet_device_info` – identity labels from generic properties (`manufacturer`, `product_code`, `uid`) with constant value `1`.

### Battery (`storage_battery`)

- `echonet_battery_state_of_capacity_percent` – state of charge (0–100).
- `echonet_battery_charge_discharge_power_watts` – instantaneous power (positive = charge, negative = discharge).
- `echonet_battery_remaining_capacity_wh` – remaining stored energy (Wh).
- `echonet_battery_chargeable_capacity_wh` / `echonet_battery_dischargeable_capacity_wh` – capacities (Wh).
- `echonet_battery_cumulative_charge_wh` / `echonet_battery_cumulative_discharge_wh` – cumulative energy (counters).
- `echonet_battery_working_operation_state` – 0x42=Charging, 0x43=Discharging, 0x44=Standby.

### Solar (`home_solar`)

- `echonet_solar_instantaneous_generation_watts` – current generation (W).
- `echonet_solar_cumulative_generation_kwh` – total generated (kWh, counter).

### AC (`home_ac`)

- `echonet_ac_operation_status` – 0x30=ON, 0x31=OFF.
- `echonet_ac_indoor_temperature_celsius` – room temperature (°C).
- `echonet_ac_set_temperature_celsius` – target temperature (°C).
- `echonet_ac_operation_mode` – raw operation mode code from EPC `0xB0`.

Common `echonet_ac_operation_mode` codes for home AC:
- `0x41` = auto
- `0x42` = cool
- `0x43` = heat
- `0x44` = dry
- `0x45` = fan_only
- `0x40` = other

Vendors may expose additional mode codes, so this metric intentionally exports the raw value.
When `enum` mappings are defined in YAML, companion one-hot metrics are exported. For example:
- `echonet_ac_operation_mode_is_auto`
- `echonet_ac_operation_mode_is_cool`

## VictoriaMetrics scrape config

Add a scrape job to your VictoriaMetrics (or single-node) config, for example:

```yaml
scrape_configs:
  - job_name: echonet
    static_configs:
      - targets: ['localhost:9191']
    metrics_path: /metrics
    scrape_interval: 60s
    scrape_timeout: 30s
```

If the exporter runs on another host, replace `localhost` with that host’s address.

## Device specs

Device classes (metrics, EPCs, units) are defined in YAML files under `internal/specs/devices/`. Each file is one device class; the filename (without `.yaml`) is the class id used in `ECHONET_DEVICES`.

**Adding a new device:** Add a YAML file in `internal/specs/devices/` and configure devices with that class. No code changes needed.

### Detached scraping

The exporter uses *detached scraping*: background goroutines poll devices at configurable intervals, and `/metrics` serves cached values. This avoids overloading low-CPU devices (e.g. ESP32-based EP Cube) when Prometheus scrapes frequently.

- **default_scrape_interval** (device-level): Default for all metrics, e.g. `1m`. Optional; defaults to 1 minute.
- **scrape_interval** (per-metric): Override for individual EPCs. Use longer intervals (e.g. `5m`, `10m`) for slow-changing or CPU-heavy properties.
- **scrape_interval** (per-device, in `ECHONET_DEVICES`): Runtime override for a specific device instance, e.g. `{"name":"epcube","ip":"192.168.1.10","class":"storage_battery","scrape_interval":"2m"}`.

Metrics with the same interval are batched into one ECHONET Get request per tick.
When supported by the device, only readable EPCs from `GETMAP` are polled.

**Example** (`internal/specs/devices/heat_pump.yaml`):

```yaml
eoj: [0x01, 0x35, 0x01]
description: "Heat pump water heater"
default_scrape_interval: 1m

metrics:
  - epc: 0x80
    name: operation_status
    help: "Operation status."
    size: 1
    scale: 1
    type: gauge
  - epc: 0xE0
    name: hot_water_temperature_celsius
    help: "Hot water temperature in °C."
    size: 2
    scale: 0.1
    invalid: 0x7FFF
    type: gauge
```

**Fields:**
- `eoj` – ECHONET Object (3 bytes: group, class, instance).
- `default_scrape_interval` – optional, e.g. `1m`, `30s`. Default for metrics without `scrape_interval`.
- `metrics` – list of EPCs to poll. Each: `epc` (hex), `name`, `help`, `size` (1/2/4 bytes), `scale` (multiplier), `type` (gauge/counter), optional `signed`, optional `invalid` (raw value meaning invalid), optional `enum` map (`raw_value: label`) for one-hot enum metrics, optional `scrape_interval` (e.g. `5m` for less frequent polling).

**Custom devices dir:** Set `ECHONET_DEVICES_DIR` to load additional YAML files from a directory (e.g. for local-only device specs without modifying the repo).

## Protocol

The exporter talks ECHONET Lite over UDP (port 3610). It sends Get requests for the properties needed per device class and parses Get_Res responses. Design follows the ECHONET Lite specification and EP Cube behaviour; other compatible devices (same object/EPC definitions) should work with the same classes.
