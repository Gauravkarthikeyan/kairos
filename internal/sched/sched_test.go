package sched

import (
	"testing"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/model"
)

func step(v float64) model.Curve { return model.Curve{Kind: "step", Value: v} }

// A long cheap job is due first; a short valuable job is due just after. EDF
// runs the long one and loses the valuable one; the value-aware policies don't.
func TestValueBeatsDeadlineOrder(t *testing.T) {
	s := time.Second
	ready := []*Task{
		{ID: 0, Class: "cheap", Deadline: 10 * s, Est: 10 * s, Curve: step(1)},
		{ID: 1, Class: "valuable", Deadline: 11 * s, Est: 2 * s, Curve: step(100)},
	}
	free := []time.Duration{0}
	if got := (EDF{}).Pick(0, ready, free); got != 0 {
		t.Fatalf("edf should pick the earlier deadline, got %d", got)
	}
	for _, p := range []Policy{Density{}, Rollout{}} {
		if got := p.Pick(0, ready, free); got != 1 {
			t.Errorf("%s picked %d, want the valuable job", p.Name(), got)
		}
	}
}

func TestShedOnlyDroppableAndHopeless(t *testing.T) {
	s := time.Second
	late := &Task{Deadline: 1 * s, Est: 5 * s, Curve: step(10), Droppable: true}
	must := &Task{Deadline: 1 * s, Est: 5 * s, Curve: step(10)}
	ontime := &Task{Deadline: 100 * s, Est: 5 * s, Curve: step(10), Droppable: true}
	sh := Shed{Policy: EDF{}}
	if !sh.Hopeless(0, late) || sh.Hopeless(0, must) || sh.Hopeless(0, ontime) {
		t.Fatal("shedding rules wrong")
	}
}

func TestFIFOOrder(t *testing.T) {
	ready := []*Task{{ID: 1, Release: 5}, {ID: 0, Release: 2}}
	if got := (FIFO{}).Pick(10, ready, []time.Duration{10}); got != 1 {
		t.Fatalf("got %d", got)
	}
}
