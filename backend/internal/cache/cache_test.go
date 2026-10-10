package cache

import (
	"errors"
	"testing"
	"time"
)

func TestValueTTL(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &Value[int]{TTL: 15 * time.Second, Now: func() time.Time { return clock }}

	loads := 0
	load := func() (int, error) { loads++; return loads, nil }

	if v, _, hit, _ := c.Get(load); v != 1 || hit {
		t.Fatalf("first get: v=%d hit=%v", v, hit)
	}
	clock = clock.Add(14 * time.Second)
	if v, _, hit, _ := c.Get(load); v != 1 || !hit {
		t.Fatalf("within TTL should hit: v=%d hit=%v", v, hit)
	}
	clock = clock.Add(2 * time.Second) // 16 s after the load → expired
	if v, _, hit, _ := c.Get(load); v != 2 || hit {
		t.Fatalf("after TTL should reload: v=%d hit=%v", v, hit)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	c := &Value[int]{TTL: time.Minute}
	if _, _, _, err := c.Get(func() (int, error) { return 0, errors.New("boom") }); err == nil {
		t.Fatal("expected error")
	}
	if v, _, hit, err := c.Get(func() (int, error) { return 7, nil }); err != nil || hit || v != 7 {
		t.Fatalf("after an error the next call must load again: v=%d hit=%v err=%v", v, hit, err)
	}
}

func TestInvalidate(t *testing.T) {
	c := &Value[int]{TTL: time.Minute}
	n := 0
	load := func() (int, error) { n++; return n, nil }
	c.Get(load)
	c.Invalidate()
	if v, _, hit, _ := c.Get(load); hit || v != 2 {
		t.Fatalf("invalidate should force a reload: v=%d hit=%v", v, hit)
	}
}
