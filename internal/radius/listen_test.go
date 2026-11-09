package radius

import (
	"context"
	"log/slog"
	"net"
	"runtime"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

// lanIPv4 returns a non-loopback IPv4 address of this machine, or skips.
func lanIPv4(t *testing.T) string {
	t.Helper()
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			return ipn.IP.String()
		}
	}
	t.Skip("no non-loopback IPv4 address")
	return ""
}

// A router drops a reply whose source address is not the address it sent the
// request to (RFC 2865). The server listens on the wildcard address; on a host
// with several addresses the kernel could pick another source. The client here is
// a connected UDP socket, which drops such a reply, like the router does.
func TestReplySourceIsRequestDestination(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject", true: "accept"}[accept], func(t *testing.T) { replySource(t, accept) })
	}
}

func replySource(t *testing.T, accept bool) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows gives no destination address, so replies use the plain socket (see Listen)")
	}
	lan := lanIPv4(t)
	conn, err := Listen(":0", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	fa := &fakeAuth{verdict: auth.Verdict{Reason: domain.ReasonInvalidCredentials}}
	want := radius.CodeAccessReject
	if accept {
		fa.verdict = auth.Verdict{Grant: domain.Grant{InterimSeconds: 60, SessionTimeout: 99}}
		want = radius.CodeAccessAccept
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	s := New(staticSecret("s3cret"), fa, &fakeAcct{}, &fakeLink{}, slog.Default())
	acct, _ := net.ListenPacket("udp", "127.0.0.1:0")
	s.Serve(conn, acct)
	t.Cleanup(func() { s.Shutdown(context.Background()) })

	// The request goes from 127.0.0.1 to the LAN address. The route back to
	// 127.0.0.1 would make a plain wildcard socket answer from 127.0.0.1.
	c, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, &net.UDPAddr{IP: net.ParseIP(lan), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := pap("alice", "pw")
	p.Add(rfc2865.NASIdentifier_Type, radius.Attribute("x"))
	raw, _ := p.Encode()
	if _, err := c.Write(raw); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("no reply from the address the request was sent to (%s): %v", lan, err)
	}
	if radius.Code(buf[0]) != want || n < 20 {
		t.Fatalf("reply code %d", buf[0])
	}
}
