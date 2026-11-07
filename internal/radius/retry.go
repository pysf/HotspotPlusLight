package radius

import (
	"sync"
	"time"
)

const (
	// retryWindow is how long a request is remembered as already counted (BR-27).
	retryWindow = 60 * time.Second
	// retryCapacity is how many requests are remembered at most. The oldest is dropped first.
	retryCapacity = 1024
)

// retryKey identifies one request as the router sends it again and again: same source, Identifier and Request Authenticator.
type retryKey struct {
	from string
	id   byte
	auth [16]byte
}

type retryEntry struct {
	key retryKey
	at  time.Time
}

// retryCache remembers recently counted requests, so a retransmitted reject is counted once (BR-27).
// It lives in memory only and is safe for concurrent use.
type retryCache struct {
	mu    sync.Mutex
	seen  map[retryKey]time.Time
	order []retryEntry // oldest first
}

func newRetryCache() *retryCache { return &retryCache{seen: map[retryKey]time.Time{}} }

// repeated reports whether k was already counted within the last retryWindow. If not, it remembers k at now.
// A repeat does not renew the time: 60 seconds after the first count the same packet counts again.
func (c *retryCache) repeated(k retryKey, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.order) > 0 && (len(c.order) >= retryCapacity || now.Sub(c.order[0].at) >= retryWindow) {
		if old := c.order[0]; c.seen[old.key].Equal(old.at) { // an older duplicate entry must not delete a newer one
			delete(c.seen, old.key)
		}
		c.order = c.order[1:]
	}
	if at, ok := c.seen[k]; ok && now.Sub(at) < retryWindow {
		return true
	}
	c.seen[k] = now
	c.order = append(c.order, retryEntry{k, now})
	return false
}

// forget removes k, so the next retransmission of it counts again. It is used when the count could not be written.
func (c *retryCache) forget(k retryKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.seen, k) // the stale order entry is skipped by the age check in repeated
}
