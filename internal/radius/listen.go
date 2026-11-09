package radius

import (
	"log/slog"
	"net"

	"golang.org/x/net/ipv4"
)

// Listen opens a UDP socket for RADIUS on addr. For ":port" and "0.0.0.0:port"
// the socket is IPv4 only and each reply leaves from the address the request was
// sent to (RouterOS drops a reply from another address).
func Listen(addr string, log *slog.Logger) (net.PacketConn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || (host != "" && host != "0.0.0.0") {
		return net.ListenPacket("udp", addr)
	}
	c, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return nil, err
	}
	p := ipv4.NewPacketConn(c)
	if err := p.SetControlMessage(ipv4.FlagDst, true); err != nil {
		log.Info("RADIUS socket: the system gives no destination address, so replies use the plain socket", "addr", addr, "err", err)
		return c, nil
	}
	log.Info("RADIUS socket is IPv4 only; \"[::]:"+port+"\" gives dual stack but without the reply-source fix", "addr", addr)
	return &pktinfoConn{PacketConn: c, p: p}, nil
}

// pktinfoConn remembers, per datagram, the address it was sent to. The remote
// address it returns carries that, so WriteTo can answer from it. The interface
// index is not sent back: the reply must follow the routing table to the router
// (on macOS an index from the request sent replies to the wrong interface), only
// the source address is set.
type pktinfoConn struct {
	net.PacketConn
	p *ipv4.PacketConn
}

// pktAddr is the sender of a datagram plus the local address it was sent to.
type pktAddr struct {
	*net.UDPAddr
	dst net.IP
}

func (c *pktinfoConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, cm, src, err := c.p.ReadFrom(b)
	if err != nil {
		return n, src, err
	}
	if cm != nil && cm.Dst != nil {
		if u, ok := src.(*net.UDPAddr); ok {
			return n, &pktAddr{UDPAddr: u, dst: cm.Dst}, nil
		}
	}
	return n, src, nil
}

func (c *pktinfoConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if pa, ok := addr.(*pktAddr); ok {
		n, err := c.p.WriteTo(b, &ipv4.ControlMessage{Src: pa.dst}, pa.UDPAddr)
		if err == nil {
			return n, nil
		}
		// Forcing the source can fail (for example the address was removed); a plain send may still work.
		return c.PacketConn.WriteTo(b, pa.UDPAddr)
	}
	return c.PacketConn.WriteTo(b, addr)
}
