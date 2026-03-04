package echonet

import (
	"encoding/hex"
	"math"
	"testing"

	"github.com/sty/echonet-exporter/internal/specs"
)

func TestParseEDT(t *testing.T) {
	invalidVal := 0x7FFF

	tests := []struct {
		name     string
		edtHex   string
		spec     specs.MetricSpec
		wantVal  float64
		wantOk   bool
	}{
		{
			name:   "1 byte unsigned",
			edtHex: "42",
			spec:   specs.MetricSpec{Size: 1, Scale: 1},
			wantVal: 66, // 0x42
			wantOk:  true,
		},
		{
			name:   "1 byte scaled",
			edtHex: "42",
			spec:   specs.MetricSpec{Size: 1, Scale: 0.1},
			wantVal: 6.6,
			wantOk:  true,
		},
		{
			name:   "2 byte unsigned",
			edtHex: "0100", // 256
			spec:   specs.MetricSpec{Size: 2, Scale: 1},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:   "2 byte signed positive",
			edtHex: "0100",
			spec:   specs.MetricSpec{Size: 2, Scale: 1, Signed: true},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:   "2 byte signed negative",
			edtHex: "FFFF", // -1
			spec:   specs.MetricSpec{Size: 2, Scale: 1, Signed: true},
			wantVal: -1,
			wantOk:  true,
		},
		{
			name:   "2 byte invalid match",
			edtHex: "7FFF",
			spec:   specs.MetricSpec{Size: 2, Scale: 1, Invalid: &invalidVal},
			wantVal: 0,
			wantOk:  false,
		},
		{
			name:   "4 byte unsigned",
			edtHex: "00000100", // 256
			spec:   specs.MetricSpec{Size: 4, Scale: 1},
			wantVal: 256,
			wantOk:  true,
		},
		{
			name:   "4 byte signed negative",
			edtHex: "FFFFFF00", // -256
			spec:   specs.MetricSpec{Size: 4, Scale: 1, Signed: true},
			wantVal: -256,
			wantOk:  true,
		},
		{
			name:   "too short",
			edtHex: "01",
			spec:   specs.MetricSpec{Size: 2, Scale: 1},
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
