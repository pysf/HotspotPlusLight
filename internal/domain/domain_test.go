package domain

import "testing"

func TestValidateAdminPassword(t *testing.T) { // BR-20
	cases := []struct {
		name, pw string
		ok       bool
	}{
		{"good", "Abcdefghijklmn1!xyz", true},
		{"exactly16", "Abcdefghijklm1!x", true},
		{"short15", "Abcdefghijkl1!x", false},
		{"no upper", "abcdefghijklmn1!xyz", false},
		{"no lower", "ABCDEFGHIJKLMN1!XYZ", false},
		{"no digit", "Abcdefghijklmnop!xyz", false},
		{"no symbol", "Abcdefghijklmnop1xyz", false},
		{"equals username", "admin", false},
	}
	for _, c := range cases {
		err := ValidateAdminPassword(c.pw, "admin")
		if (err == nil) != c.ok {
			t.Errorf("%s: got err=%v, want ok=%v", c.name, err, c.ok)
		}
	}
	if ValidateAdminPassword("Abcdefghijklmn1!xyz", "Abcdefghijklmn1!xyz") == nil {
		t.Error("password equal to username must be rejected")
	}
}
