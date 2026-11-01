package member

import (
	"context"
	"testing"
)

type fakeRepo struct{ n int }

func (f fakeRepo) CountMembers(context.Context) (int, error) { return f.n, nil }

func TestCount(t *testing.T) {
	if n, err := New(fakeRepo{3}).Count(context.Background()); err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
}
