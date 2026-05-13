package ratelimit_test

import (
	"testing"

	"github.com/tesserakdev/tsk/internal/ratelimit"
)

func TestAllow_WithinLimit(t *testing.T) {
	l := ratelimit.New(3)
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Fatalf("call %d should be allowed, got rejected", i+1)
		}
	}
}

func TestAllow_ExceedLimit(t *testing.T) {
	l := ratelimit.New(3)
	for i := 0; i < 3; i++ {
		l.Allow()
	}
	if l.Allow() {
		t.Fatal("4th call should be rejected, got allowed")
	}
}

func TestAllow_ZeroMeansUnlimited(t *testing.T) {
	l := ratelimit.New(0)
	for i := 0; i < 1000; i++ {
		if !l.Allow() {
			t.Fatalf("call %d should be allowed with unlimited limiter", i+1)
		}
	}
}

func TestAllow_LimitOfOne(t *testing.T) {
	l := ratelimit.New(1)
	if !l.Allow() {
		t.Fatal("first call should be allowed")
	}
	if l.Allow() {
		t.Fatal("second call should be rejected")
	}
}
