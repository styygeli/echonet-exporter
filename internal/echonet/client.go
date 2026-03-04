package echonet

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/model"
	"github.com/sty/echonet-exporter/internal/specs"
)

const (
	echonetPort    = 3610
	ehd1           = 0x10
	ehd2           = 0x81
	esvGet         = 0x62
	esvGetRes      = 0x72
	seojController = 0x05
	seojClass      = 0xFF
	seojInstance   = 0x01
	minResponseLen = 12
)

// Client sends ECHONET Lite Get requests over UDP and parses Get_Res.
type Client struct {
	timeout time.Duration
	specs   map[string]*specs.DeviceSpec
}

// NewClient creates a client with the given scrape timeout and device specs.
func NewClient(timeoutSec int, deviceSpecs map[string]*specs.DeviceSpec) *Client {
	return &Client{
		timeout: time.Duration(timeoutSec) * time.Second,
		specs:   deviceSpecs,
	}
}

// GetRequest builds an ECHONET Lite Get frame.
func GetRequest(tid uint16, eoj [3]byte, epcs []byte) []byte {
	n := 4 + 2 + 3 + 3 + 1 + 1 + 2*len(epcs)
	b := make([]byte, 0, n)
	b = append(b, ehd1, ehd2)
	b = append(b, byte(tid>>8), byte(tid))
	b = append(b, seojController, seojClass, seojInstance)
	b = append(b, eoj[0], eoj[1], eoj[2])
	b = append(b, esvGet)
	b = append(b, byte(len(epcs)))
	for _, epc := range epcs {
		b = append(b, epc, 0)
	}
	return b
}

// SendGet sends a Get request to addr and returns the raw response.
func (c *Client) SendGet(addr string, eoj [3]byte, epcs []byte) ([]byte, error) {
	if len(epcs) == 0 {
		return nil, fmt.Errorf("no EPCs")
	}
	tid := uint16(time.Now().UnixNano() & 0xFFFF)
	req := GetRequest(tid, eoj, epcs)

	host := addr
	if _, _, err := net.SplitHostPort(addr); err != nil {
		host = net.JoinHostPort(addr, fmt.Sprint(echonetPort))
	}

	conn, err := net.DialTimeout("udp", host, c.timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return nil, err
	}

	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// ParseGetRes parses an ECHONET Lite frame and returns properties if it is a Get_Res.
func ParseGetRes(data []byte) (tid uint16, props []model.GetResProperty, err error) {
	if len(data) < minResponseLen {
		return 0, nil, fmt.Errorf("response too short: %d", len(data))
	}
	if data[0] != ehd1 || data[1] != ehd2 {
		return 0, nil, fmt.Errorf("invalid EHD: %02x %02x", data[0], data[1])
	}
	tid = binary.BigEndian.Uint16(data[2:4])
	esv := data[10]
	if esv != esvGetRes {
		return 0, nil, fmt.Errorf("not Get_Res: ESV=%02x", esv)
	}
	opc := int(data[11])
	pos := 12
	for i := 0; i < opc && pos+2 <= len(data); i++ {
		epc := data[pos]
		pdc := data[pos+1]
		pos += 2
		edtLen := int(pdc)
		if pos+edtLen > len(data) {
			break
		}
		edt := make([]byte, edtLen)
		copy(edt, data[pos:pos+edtLen])
		pos += edtLen
		props = append(props, model.GetResProperty{EPC: epc, PDC: pdc, EDT: edt})
	}
	return tid, props, nil
}

// DeviceResult holds parsed metrics for one device scrape (metric name -> value).
type DeviceResult struct {
	Success     bool
	DurationSec float64
	Metrics     map[string]MetricValue
}

// MetricValue holds a parsed value and its type (gauge or counter).
type MetricValue struct {
	Value float64
	Type  string // "gauge" or "counter"
}

func prop(props []model.GetResProperty, epc byte) ([]byte, bool) {
	for _, p := range props {
		if p.EPC == epc && len(p.EDT) > 0 {
			return p.EDT, true
		}
	}
	return nil, false
}

func parseEDT(edt []byte, m specs.MetricSpec) (float64, bool) {
	if len(edt) < m.Size {
		return 0, false
	}
	var raw int64
	switch m.Size {
	case 1:
		raw = int64(edt[0])
	case 2:
		u := binary.BigEndian.Uint16(edt)
		if m.Invalid != nil && int(u) == *m.Invalid {
			return 0, false
		}
		if m.Signed {
			raw = int64(int16(u))
		} else {
			raw = int64(u)
		}
	case 4:
		u := binary.BigEndian.Uint32(edt)
		if m.Signed {
			raw = int64(int32(u))
		} else {
			raw = int64(u)
		}
	default:
		return 0, false
	}
	return float64(raw) * m.Scale, true
}

// ScrapeDevice runs a Get for the device and returns parsed metrics from the spec.
func (c *Client) ScrapeDevice(dev config.Device) (DeviceResult, error) {
	start := time.Now()
	spec, ok := c.specs[dev.Class]
	if !ok || spec == nil {
		return DeviceResult{Success: false, DurationSec: time.Since(start).Seconds()}, fmt.Errorf("unknown class: %s", dev.Class)
	}

	epcs := make([]byte, 0, len(spec.Metrics))
	for _, m := range spec.Metrics {
		epcs = append(epcs, m.EPC)
	}

	raw, err := c.SendGet(dev.IP, spec.EOJ, epcs)
	var out DeviceResult
	out.DurationSec = time.Since(start).Seconds()
	out.Metrics = make(map[string]MetricValue)
	if err != nil {
		return out, err
	}

	_, props, err := ParseGetRes(raw)
	if err != nil {
		return out, err
	}
	out.Success = true

	for _, m := range spec.Metrics {
		edt, ok := prop(props, m.EPC)
		if !ok {
			continue
		}
		v, ok := parseEDT(edt, m)
		if !ok {
			continue
		}
		out.Metrics[m.Name] = MetricValue{Value: v, Type: m.Type}
	}

	return out, nil
}
