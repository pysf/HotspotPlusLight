package radius

import (
	"crypto/hmac"
	"crypto/md5"

	"layeh.com/radius"
	"layeh.com/radius/rfc2869"
)

// Message-Authenticator (attribute 80, RFC 3579 3.2) is an HMAC-MD5 over the whole
// packet, keyed with the shared secret. The Message-Authenticator value counts as
// 16 zero bytes while it is computed.
//
// Why we need it: RouterOS 7.15+ answers the BlastRADIUS attack (CVE-2024-3596)
// by sending it in Access-Requests and, with `require-message-auth`
// set, silently dropping replies that do not carry it. layeh.com/radius
// never adds it, so the router ignored every Accept and Reject (docs/radius-reply-bug.md).
//
// VERIFY-ON-HARDWARE: the RouterOS option name and default (PLAN 13).
//
// DECISION: a request without Message-Authenticator is still accepted (older
// routers do not send it). A future setting could require it.

const maLen = 16

// zeroedMA returns p's attributes with every Message-Authenticator value set to
// 16 zero bytes. p is not changed.
func zeroedMA(p *radius.Packet) radius.Attributes {
	out := make(radius.Attributes, len(p.Attributes))
	for i, a := range p.Attributes {
		c := *a
		if a.Type == rfc2869.MessageAuthenticator_Type {
			c.Attribute = make(radius.Attribute, maLen)
		}
		out[i] = &c
	}
	return out
}

// computeMA returns the Message-Authenticator for p. authenticator is the value
// of the packet's Authenticator field at the time of the calculation:
//   - Access-Request: its own Request Authenticator.
//   - Access-Accept/Reject/Challenge: the REQUEST's authenticator (RFC 3579 3.2).
//   - Disconnect/CoA-Request: 16 zero bytes. The Request Authenticator of these is
//     an MD5 over the packet including the Message-Authenticator, so the
//     Message-Authenticator must be computed first (RFC 5176 2.3, RFC 3579 3.2).
func computeMA(p *radius.Packet, authenticator [16]byte) ([]byte, error) {
	q := &radius.Packet{Code: p.Code, Identifier: p.Identifier, Authenticator: authenticator, Attributes: zeroedMA(p)}
	b, err := q.MarshalBinary()
	if err != nil {
		return nil, err
	}
	h := hmac.New(md5.New, p.Secret)
	h.Write(b)
	return h.Sum(nil), nil
}

// sealMA puts a valid Message-Authenticator as the FIRST attribute of p (the
// BlastRADIUS advice: first, so no earlier attribute can be forged).
// Call it last, after every other attribute is set. An old one is replaced.
func sealMA(p *radius.Packet, authenticator [16]byte) error {
	p.Attributes.Del(rfc2869.MessageAuthenticator_Type)
	p.Attributes = append(radius.Attributes{{Type: rfc2869.MessageAuthenticator_Type, Attribute: make(radius.Attribute, maLen)}}, p.Attributes...)
	sum, err := computeMA(p, authenticator)
	if err != nil {
		return err
	}
	p.Attributes[0].Attribute = sum
	return nil
}

// checkMA reports whether p carries a Message-Authenticator (present) and whether
// it is correct (ok). More than one, or one of the wrong length, is not ok.
func checkMA(p *radius.Packet, authenticator [16]byte) (present, ok bool) {
	var got []radius.Attribute
	for _, a := range p.Attributes {
		if a.Type == rfc2869.MessageAuthenticator_Type {
			got = append(got, a.Attribute)
		}
	}
	switch {
	case len(got) == 0:
		return false, true
	case len(got) > 1 || len(got[0]) != maLen:
		return true, false
	}
	sum, err := computeMA(p, authenticator)
	return true, err == nil && hmac.Equal(sum, got[0])
}
