package estimate

import (
	"testing"
	"time"
)

func TestEstimate(t *testing.T) {
	e := New(5, 0.8)
	if got := e.Estimate("x", 7*time.Second); got != 7*time.Second {
		t.Fatalf("hint expected, got %v", got)
	}
	for _, s := range []int{1, 2, 3, 4, 5} {
		e.Observe("x", time.Duration(s)*time.Second)
	}
	if got := e.Estimate("x", 0); got != 4*time.Second {
		t.Fatalf("p80 of 1..5 should be 4s, got %v", got)
	}
	e.Observe("x", 100*time.Second) // window slides: 2,3,4,5,100
	if got := e.Estimate("x", 0); got != 5*time.Second {
		t.Fatalf("got %v", got)
	}
}
