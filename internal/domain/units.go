package domain

import (
	"fmt"
	"math"
)

// Speed and bulk units (BR-4). Values may be decimal.
var (
	SpeedUnits = []string{"Kbps", "Mbps", "Gbps"}
	BulkUnits  = []string{"KB", "MB", "GB"}
)

// SpeedKbps converts a speed in the given unit to kilobits per second
// (Kbps x1, Mbps x1000, Gbps x1,000,000). Legacy: utility.toKbps.
func SpeedKbps(value float64, unit string) (float64, error) {
	switch unit {
	case "Kbps":
		return value, nil
	case "Mbps":
		return value * 1000, nil
	case "Gbps":
		return value * 1_000_000, nil
	}
	return 0, fmt.Errorf("unknown speed unit %q", unit)
}

// BulkBytes converts a data amount to bytes (KB x1024, MB x1024^2, GB x1024^3),
// rounded to the nearest byte. Legacy: utility.toByte.
func BulkBytes(value float64, unit string) (int64, error) {
	var f float64
	switch unit {
	case "KB":
		f = 1 << 10
	case "MB":
		f = 1 << 20
	case "GB":
		f = 1 << 30
	default:
		return 0, fmt.Errorf("unknown bulk unit %q", unit)
	}
	r := math.Round(value * f)
	if r >= math.MaxInt64 { // saturate instead of overflowing
		return math.MaxInt64, nil
	}
	return int64(r), nil
}
