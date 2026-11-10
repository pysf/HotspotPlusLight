package update

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// HealthTimeout is how long the new version has to become healthy.
const HealthTimeout = 60 * time.Second

// HealthURL returns the /healthz URL for the admin listener address addr. An address without a host,
// or with an unspecified one such as 0.0.0.0 or [::], is reached through the loopback address.
func HealthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/healthz"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// healthClient talks to our own listener. It never uses a proxy from the environment and never follows a redirect.
var healthClient = &http.Client{
	Transport:     &http.Transport{Proxy: nil},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// ProbeHealth does one GET of url and fails unless the answer is 200.
func ProbeHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := healthClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %d", resp.StatusCode)
	}
	return nil
}

// WaitHealthy calls probe every interval until it succeeds, timeout passes or ctx ends.
// It returns nil when the program is healthy and the last error otherwise.
func WaitHealthy(ctx context.Context, probe func(context.Context) error, timeout, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		err := probe(ctx)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(interval):
		}
	}
}
