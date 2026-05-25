package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUint256UnmarshalUsesCanonicalDecimalString(t *testing.T) {
	const maxUint256 = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	tests := []struct {
		name string
		raw  string
		want Uint256
	}{
		{name: "decimal string", raw: `"123"`, want: "123"},
		{name: "decimal number", raw: `123`, want: "123"},
		{name: "hex string", raw: `"0x7b"`, want: "123"},
		{name: "padded hex string", raw: `"0x00007b"`, want: "123"},
		{name: "max", raw: `"` + maxUint256 + `"`, want: maxUint256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Uint256
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Uint256 = %q, want %q", got, tt.want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != `"`+tt.want.String()+`"` {
				t.Fatalf("encoded Uint256 = %s, want quoted decimal %q", encoded, tt.want)
			}
		})
	}
}

func TestUint256UnmarshalRejectsInvalidValues(t *testing.T) {
	overflow := "1" + strings.Repeat("0", 78)
	for _, raw := range []string{`"-1"`, `"not-a-number"`, `"0x"`, `"` + overflow + `"`} {
		t.Run(raw, func(t *testing.T) {
			var got Uint256
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("expected %s to fail", raw)
			}
		})
	}
}
