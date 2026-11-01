package voucher

import (
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
)

func TestNew(t *testing.T) {
	if New(clock.NewFake(time.Unix(0, 0))) == nil {
		t.Fatal("nil service")
	}
}
