package dbdecimal

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNumericRangeAndRounding(t *testing.T) {
	for _, tt := range []struct {
		input, want string
		p, s        int32
		fails       bool
	}{
		{"9999999999.9999999999", "9999999999.9999999999", 20, 10, false},
		{"999999999999.99999999", "999999999999.99999999", 20, 8, false},
		{"0.00000000005", "0.0000000001", 20, 10, false},
		{"-0.00000000005", "-0.0000000001", 20, 10, false},
		{"-0.00000000001", "0.0000000000", 20, 10, false},
		{"9999999999.99999999995", "", 20, 10, true},
		{"1000000000000", "", 20, 8, true},
	} {
		t.Run(tt.input, func(t *testing.T) {
			d, err := Parse(tt.input)
			require.NoError(t, err)
			got, err := d.Quantize(tt.p, tt.s)
			if tt.fails {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
				require.True(t, Canonical(got, tt.p, tt.s))
			}
		})
	}
}

func TestScannerRejectsInexactStorage(t *testing.T) {
	d, err := Parse("1.25")
	require.NoError(t, err)
	for _, value := range []any{float64(1.25), nil, "NaN", "Infinity", "1e999999"} {
		require.Error(t, d.Scan(value))
		require.Equal(t, "1.25", d.String())
	}
	require.NoError(t, d.Scan([]byte("9999999999.9999999999")))
	value, err := d.Value()
	require.NoError(t, err)
	require.Equal(t, "9999999999.9999999999", value)
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := FromFloat(value)
		require.Error(t, err)
	}
}
