package e2e

import (
	"testing"
	"time"

	"layeh.com/radius"

	"hotspotpluslight/e2e/fakenas"
)

// E15.01 a request with a wrong Message-Authenticator is dropped without a reply;
// a good one is answered with a reply that carries a correct Message-Authenticator
// as its first attribute (RouterOS 7.15+ drops replies without it; docs/radius-reply-bug.md).
func TestE15_01_MessageAuthenticatorOnAuth(t *testing.T) {
	f := newP5(t)
	good := f.nas.PAPPacket("nobody", "pw")
	reply, err := f.nas.Exchange(good) // Exchange fails unless the reply has a correct first MA
	if err != nil || reply.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", reply, err)
	}
	bad := f.nas.PAPPacket("nobody", "pw")
	bad.Attributes[len(bad.Attributes)-1].Attribute[0] ^= 0xff
	if silent, err := f.nas.Silent(bad); err != nil || !silent {
		t.Fatalf("a bad Message-Authenticator must get no reply: silent=%v err=%v", silent, err)
	}
}

// E15.02 a router without Message-Authenticator (older RouterOS) is still served.
func TestE15_02_NoMessageAuthenticatorStillServed(t *testing.T) {
	f := newP5(t)
	f.nas.MessageAuth = false
	if r, err := f.nas.Authenticate("nobody", "pw"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("%v %v", r, err)
	}
}

// E15.03 the Disconnect-Request carries a correct Message-Authenticator, and a
// reply with a wrong one is not believed.
func TestE15_03_DisconnectMessageAuthenticator(t *testing.T) {
	f := newP5(t)
	plan := f.plan("Basic")
	f.member(plan, "alice")
	f.online("alice", "sess-1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	id := f.sessionDBID("sess-1")
	if r := f.c.Send("POST", spath(id), nil); r.StatusCode != 200 {
		t.Fatalf("kick: %d %s", r.StatusCode, r.Body)
	}
	d := f.coa.Requests()[0]
	if !d.Authentic || !d.MAPresent || !d.MAValid {
		t.Fatalf("authentic=%v MA present=%v valid=%v", d.Authentic, d.MAPresent, d.MAValid)
	}

	f.member(plan, "bob")
	f.online("bob", "sess-2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.coa.Always(fakenas.ACKBadMA)
	if r := f.c.Send("POST", spath(f.sessionDBID("sess-2")), nil); r.StatusCode == 200 {
		t.Fatalf("a reply with a wrong Message-Authenticator was accepted: %s", r.Body)
	}
}

// E16.05 (review A3) a request with a valid Message-Authenticator proves the shared secret, so a password with a
// tab or a non-UTF-8 byte gets a normal reply and no wrong-secret alarm. Without one, the old heuristics stay (E8.07).
func TestE16_05_ValidMADisablesWrongSecretHeuristics(t *testing.T) {
	f := newP5(t)
	for _, pw := range []string{"pw\tx", "pw\xff\xfe"} {
		r, err := f.nas.Authenticate("nobody", pw)
		if err != nil || r.Code != radius.CodeAccessReject {
			t.Fatalf("password %q: want a normal reject, got %v %v", pw, r, err)
		}
	}
	d := f.c.Get("/admin/router").Doc(t)
	if n := text(d.Find("[data-bad-secret-count]")); n != "0" {
		t.Errorf("bad secret count %q, want 0", n)
	}
}

// E16.05b (review A3) the opposite case: a wrong Message-Authenticator with a garbage password still raises
// the wrong-secret alarm and gets no reply. E8.07 covers only a readable password.
func TestE16_05b_WrongMAWithGarbagePasswordStillAlarms(t *testing.T) {
	f := newP5(t)
	bad := fakenas.New(f.s.Addrs.RadiusAuth, "wrong-secret")
	bad.Timeout = 300 * time.Millisecond
	if _, err := bad.Authenticate("nobody", "pw\xff\xfe\t"); err == nil {
		t.Fatal("a wrong-secret Access-Request was answered")
	}
	d := f.c.Get("/admin/router").Doc(t)
	if n := text(d.Find("[data-bad-secret-count]")); n != "1" {
		t.Errorf("bad secret count %q, want 1", n)
	}
}
