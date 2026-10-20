package model

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestCurveAt(t *testing.T) {
	s := time.Second
	cases := []struct {
		c    Curve
		late time.Duration
		want float64
	}{
		{Curve{Kind: "step", Value: 10}, 0, 10},
		{Curve{Kind: "step", Value: 10}, s, 0},
		{Curve{Kind: "step", Value: 10, Floor: 0.3}, s, 3},
		{Curve{Kind: "linear", Value: 10, Grace: Duration(10 * s)}, 5 * s, 5},
		{Curve{Kind: "linear", Value: 10, Grace: Duration(10 * s)}, 20 * s, 0},
		{Curve{Kind: "exp", Value: 10, Grace: Duration(s)}, s, 10 / math.E},
	}
	for i, c := range cases {
		if got := c.c.At(c.late); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestDurationJSON(t *testing.T) {
	var v struct{ A, B Duration }
	if err := json.Unmarshal([]byte(`{"A":"1m30s","B":2.5}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A.D() != 90*time.Second || v.B.D() != 2500*time.Millisecond {
		t.Fatalf("got %v %v", v.A.D(), v.B.D())
	}
}

func TestValidate(t *testing.T) {
	bad := []Curve{{Kind: "step"}, {Kind: "linear", Value: 1}, {Kind: "wat", Value: 1}, {Kind: "step", Value: 1, Floor: 1}}
	for i, c := range bad {
		if c.Validate() == nil {
			t.Errorf("case %d should be invalid", i)
		}
	}
}
