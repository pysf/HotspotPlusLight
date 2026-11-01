package domain

import "testing"

func TestSpeedKbps(t *testing.T) {
	for _, c := range []struct {
		v    float64
		u    string
		want float64
	}{{512, "Kbps", 512}, {2, "Mbps", 2000}, {1.5, "Mbps", 1500}, {1, "Gbps", 1_000_000}, {0.25, "Gbps", 250_000}, {0, "Mbps", 0}} {
		got, err := SpeedKbps(c.v, c.u)
		if err != nil || got != c.want {
			t.Errorf("%v %s: %v %v", c.v, c.u, got, err)
		}
	}
	if _, err := SpeedKbps(1, "Tbps"); err == nil {
		t.Error("unknown unit accepted")
	}
}

func TestBulkBytes(t *testing.T) {
	for _, c := range []struct {
		v    float64
		u    string
		want int64
	}{{1, "KB", 1024}, {1, "MB", 1 << 20}, {1, "GB", 1 << 30}, {0.5, "GB", 1 << 29}, {1.5, "MB", 1572864}, {100.75, "KB", 103168}, {0, "GB", 0}} {
		got, err := BulkBytes(c.v, c.u)
		if err != nil || got != c.want {
			t.Errorf("%v %s: %v %v", c.v, c.u, got, err)
		}
	}
	if _, err := BulkBytes(1, "TB"); err == nil {
		t.Error("unknown unit accepted")
	}
}
