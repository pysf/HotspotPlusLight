package clock

import (
	"testing"
	"time"
)

func TestFake(t *testing.T) {
	start := time.Unix(1000, 0)
	f := NewFake(start)
	var c Clock = f
	if !c.Now().Equal(start) {
		t.Fatal("start mismatch")
	}
	f.Advance(time.Minute)
	if got := c.Now(); !got.Equal(start.Add(time.Minute)) {
		t.Fatalf("got %v", got)
	}
	if time.Since(Real{}.Now()) > time.Second {
		t.Fatal("real clock off")
	}
}
