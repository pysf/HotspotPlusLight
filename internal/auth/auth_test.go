package auth

import (
	"context"
	"testing"

	"hotspotpluslight/internal/domain"
)

type fakeRepo struct{ a domain.Admin }

func (f fakeRepo) GetAdmin(context.Context) (domain.Admin, error) { return f.a, nil }

func TestGeneratedPasswordAlwaysSatisfiesBR20(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		p, err := GeneratePassword(20)
		if err != nil || len(p) != 20 {
			t.Fatalf("%q %v", p, err)
		}
		if err := domain.ValidateAdminPassword(p, "admin"); err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		seen[p] = true
	}
	if len(seen) < 300 {
		t.Fatal("passwords repeat")
	}
}

func TestVerifyAdmin(t *testing.T) {
	h, _ := HashPassword("Correct-Horse-9-Battery")
	s := New(fakeRepo{domain.Admin{Username: "admin", PasswordHash: h}})
	ctx := context.Background()
	for _, c := range []struct {
		u, p string
		ok   bool
	}{{"admin", "Correct-Horse-9-Battery", true}, {"admin", "nope", false}, {"root", "Correct-Horse-9-Battery", false}} {
		if ok, _ := s.VerifyAdmin(ctx, c.u, c.p); ok != c.ok {
			t.Errorf("%v: got %v", c, ok)
		}
	}
}
