package echonet

import (
	"encoding/hex"
	"math"
	"testing"

	"github.com/styygeli/echonet-exporter/internal/model"
	"github.com/styygeli/echonet-exporter/internal/specs"
)

func TestParseEDT(t *testing.T) {
	invalidVal := 0x7FFF

	tests := []struct {
		name    string
		edtHex  string
		spec    specs.MetricSpec
		wantVal float64
		wantOk  bool
	}{
		{
			name:    "1 byte unsigned",
			edtHex:  "42",
			spec:    specs.MetricSpec{Size: 1, Scale: 1},
			wantVal: 66, // 0x42
			wantOk:  true,
		},
		{
			name:    "1 byte scaled",
			edtHex:  "42",
			spec:    specs.MetricSpec{Size: 1, Scale: 0.1},
			wantVal: 6.6,
			wantOk:  true,
		},
		{
			name:    "2 byte unsigned",
			edtHex:  "0100", // 256
			spec:    specs.MetricSpec{Size: 2, Scale: 1},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:    "2 byte signed positive",
			edtHex:  "0100",
			spec:    specs.MetricSpec{Size: 2, Scale: 1, Signed: true},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:    "2 byte signed negative",
			edtHex:  "FFFF", // -1
			spec:    specs.MetricSpec{Size: 2, Scale: 1, Signed: true},
			wantVal: -1,
			wantOk:  true,
		},
		{
			name:    "2 byte invalid match",
			edtHex:  "7FFF",
			spec:    specs.MetricSpec{Size: 2, Scale: 1, Invalid: &invalidVal},
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:    "4 byte unsigned",
			edtHex:  "00000100", // 256
			spec:    specs.MetricSpec{Size: 4, Scale: 1},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:    "4 byte signed negative",
			edtHex:  "FFFFFF00", // -256
			spec:    specs.MetricSpec{Size: 4, Scale: 1, Signed: true},
			wantVal: -256,
			wantOk:  true,
		},
		{
			name:    "too short",
			edtHex:  "01",
			spec:    specs.MetricSpec{Size: 2, Scale: 1},
			wantVal: 0,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edt, err := hex.DecodeString(tt.edtHex)
			if err != nil {
				t.Fatalf("invalid hex: %v", err)
			}

			gotVal, gotOk := parseEDT(edt, tt.spec)
			if gotOk != tt.wantOk {
				t.Errorf("parseEDT() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotOk {
				if math.Abs(gotVal-tt.wantVal) > 1e-9 {
					t.Errorf("parseEDT() val = %v, want %v", gotVal, tt.wantVal)
				}
			}
		})
	}
}

func TestDecodePropertyMap_ListFormat(t *testing.T) {
	edt := []byte{3, 0x80, 0xB0, 0xBB}
	got := decodePropertyMap(edt)
	if len(got) != 3 {
		t.Fatalf("expected 3 properties, got %d", len(got))
	}
	for _, epc := range []byte{0x80, 0xB0, 0xBB} {
		if _, ok := got[epc]; !ok {
			t.Fatalf("expected EPC 0x%02X to be present", epc)
		}
	}
}

func TestDecodePropertyMap_BitmapFormat(t *testing.T) {
	edt := make([]byte, 17)
	edt[1] = 0x01  // 0x80
	edt[2] = 0x01  // 0x81
	edt[16] = 0x02 // 0x9F

	got := decodePropertyMap(edt)
	for _, epc := range []byte{0x80, 0x81, 0x9F} {
		if _, ok := got[epc]; !ok {
			t.Fatalf("expected EPC 0x%02X to be present in decoded bitmap map", epc)
		}
	}
}

func TestMissingEPCs(t *testing.T) {
	requested := []byte{0x80, 0xB0, 0xBB}
	props := []model.GetResProperty{
		{EPC: 0x80, PDC: 1, EDT: []byte{0x30}},
		{EPC: 0xBB, PDC: 2, EDT: []byte{0x00, 0x19}},
	}
	missing := missingEPCs(requested, props)
	if len(missing) != 1 || missing[0] != 0xB0 {
		t.Fatalf("expected missing [0xB0], got %#v", missing)
	}
}

func TestDecodeDeviceInfoHelpers(t *testing.T) {
	if got := decodeManufacturer([]byte{0x00, 0x01, 0x31}); got != "Sungrow" {
		t.Fatalf("decodeManufacturer: got %q want %q", got, "Sungrow")
	}
	if got := decodeProductCode([]byte("GZ-000900\x00\x00")); got != "GZ-000900" {
		t.Fatalf("decodeProductCode: got %q", got)
	}
	if got := decodeUID([]byte{0x00, 0x11, 0x22, 0x33}, "192.168.1.10"); got != "112233" {
		t.Fatalf("decodeUID from EDT: got %q", got)
	}
}
