# RADIUS replies not reaching the router (2026-11-04)

## Symptom
Wrong login: the reject was logged 3 times, 5 s apart (the router retries after
`timeout=5s`). Right login: no log line, and the router page said the login server
does not answer. So the router never accepted any reply.

## Root cause (reproduced locally)
The server listened on a wildcard UDP socket (`[::]:1812`). On a host with several
addresses (this Mac: 172.16.0.252 and 192.168.1.102, plus VPN/IPv6) the kernel
picks the source address of a reply from its routing table, not from the address
the request was sent to. RFC 2865 and RouterOS drop a reply from another address.
Test `TestReplySourceIsRequestDestination` showed it: a request sent to the LAN
address from 127.0.0.1 got no usable reply. This is a good fit for the symptom;
it is not proven on the real router (see the commands below).

Checked and ruled out in code: the shutdown rewrite (`lateCloseConn`) passes
`WriteTo` straight through; the reply uses the same secret as request checking
(a wrong-secret request is dropped before we answer); the reject has only
Reply-Message, so Accept-only attribute problems cannot explain it.

## Update: router is 172.16.0.1 (same subnet as the Mac, 172.16.0.252)
This weakens the wrong-source theory: on the same /24 the kernel normally answers
from 172.16.0.252. It stays possible if the router sends to another address of the
Mac (192.168.1.102, VPN), but it is NOT proven. The reply path in code was traced
for Accept and Reject: `reply()` has no early return after the log line, and the
only write is `w.Write`, which goes to `lateCloseConn.WriteTo` (the embedded
socket; `stopped` and the read deadline affect reads only). A failed write was
logged ("write reply"), and none appeared, so the write call itself succeeded: the
packet left the program. What happens after that (source IP, interface, router
check of the Response Authenticator) can only be seen with tcpdump on the Mac.

Next most likely causes if tcpdump shows the reply leaving from 172.16.0.252 to
172.16.0.1:
1. The router rejects the reply: Response Authenticator wrong. The server signs
   with the secret it used to parse the request, so check `/radius print detail`
   for a secret with a trailing space or a different value.
2. `/radius monitor 0` on the router: `bad-replies` growing means a reply was seen
   but invalid; `timeouts` growing means no reply was seen (firewall, interface).
3. A firewall or Little Snitch on the Mac or router dropping UDP 1812 replies.

## Status (2026-11-04): the live bug is still open
The reply-source fix and the Message-Authenticator change are in the code, but the
real problem is not solved. The router still times out while `tcpdump` on the
server sees the reply leave. So the reply is sent, but the router does not take it.
This needs checks on your side (see the commands below): is the reply source the
address the router sent to, does `/radius monitor 0` show `timeouts` or
`bad-replies`, and is a firewall or the network between them dropping it.

Message-Authenticator is not the cause. The router shows `require-message-auth=no`
and `bad-replies 0`. We added it anyway as hardening:
- Access-Accept and Access-Reject carry a correct Message-Authenticator as the first
  attribute (RouterOS 7.15+ can require it).
- The Disconnect-Request carries one. A Disconnect reply with a wrong one is dropped
  and the kick retries, like a reply with a bad Response Authenticator.
- An Access-Request with a wrong one is dropped, logged, and raises
  the wrong-secret alarm. Anyone can forge it, and the owner accepts a false alarm.

## Fix
- `radius.Listen` (`internal/radius/listen.go`): for a wildcard IPv4 address
  (`:1812`, `0.0.0.0:1812`) it opens an IPv4 socket with IP_PKTINFO and sends each
  reply from the address the request was sent to. Only the source address is set,
  not the interface (setting the interface made macOS lose the reply).
  `[::]:1812` or a specific IP gives a plain socket, as before. If the write with the
  forced source fails, it is tried once more with the plain socket.
- `internal/app` uses `radius.Listen` for both sockets.
- Tests: `TestReplySourceIsRequestDestination` (reject and accept) does a real UDP exchange with a connected client that drops replies from a wrong source.
- Logs: reply write errors are logged as errors ("radius reply not sent"); with
  `HSP_LOG_LEVEL=debug` (or the debug level you use) each request and reply is logged
  (from, to, code, id).
- The startup log now shows `0.0.0.0:1812` instead of `[::]:1812`. The server no
  longer listens for RADIUS over IPv6 on the default setting. It logs one Info line
  about this. Use `[::]:1812` for dual stack, without the reply-source fix.

## Immediate workaround (without the fix)
Set `HSP_RADIUS_AUTH_ADDR=192.168.1.102:1812` and `HSP_RADIUS_ACCT_ADDR=192.168.1.102:1813`
(the IP the router uses as the RADIUS address), and make sure the router's
`/radius` `address=` is that same IP.

## Commands to confirm
On the Mac:
- `ifconfig | grep "inet "` : list the addresses.
- `sudo tcpdump -ni any 'udp port 1812 or udp port 1813'` : the router's request
  goes to IP A; the reply must come FROM IP A. Before the fix you will see another
  source IP (or the reply leaving on another interface).
- `netstat -rn -f inet | head -20` : which route the router's IP uses.

On the router:
- `/radius print detail` : check `address=`, `secret`, `src-address`, `timeout`.
- `/radius monitor 0` : `requests`, `accepts`, `rejects`, `timeouts`, `bad-replies`.
  Before the fix: timeouts grow. After: accepts/rejects grow.
- `/system logging add topics=radius,debug` then `/log print where topics~"radius"`.
- Optional: `/radius set 0 src-address=<router IP in the LAN>`.

Also check the router's route to the Mac: the router must reach the same Mac
address it sends to (VPN or a second NIC can change it).
