package sim_test

import (
	"testing"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/model"
	"github.com/Gauravkarthikeyan/kairos/internal/sched"
	"github.com/Gauravkarthikeyan/kairos/internal/sim"
	"github.com/Gauravkarthikeyan/kairos/internal/workload"
)

func TestHandComputed(t *testing.T) {
	s := time.Second
	step := model.Curve{Kind: "step", Value: 10}
	arrs := []sim.Arrival{
		{Class: "a", Release: 0, Deadline: 5 * s, True: 4 * s, Hint: 4 * s, Curve: step},
		{Class: "b", Release: 0, Deadline: 5 * s, True: 4 * s, Hint: 4 * s, Curve: step},
	}
	r := sim.Run(arrs, sim.Config{Workers: 1, Policy: sched.FIFO{}})
	// a finishes at 4s (on time), b at 8s (3s late).
	if r.Utility != 10 || r.OnTime != 1 || r.Late != 1 || r.MeanLate != 3*s {
		t.Fatalf("%+v", r)
	}
	r = sim.Run(arrs, sim.Config{Workers: 2, Policy: sched.FIFO{}})
	if r.Utility != 20 {
		t.Fatalf("two workers should fit both: %+v", r)
	}
}

func TestInvariantsAndDeterminism(t *testing.T) {
	arrs := workload.Generate(7, 1500, 4, 1.4, workload.Default())
	for _, p := range []sched.Policy{sched.FIFO{}, sched.EDF{}, sched.Shed{Policy: sched.Density{}}, sched.Rollout{}} {
		a := sim.Run(arrs, sim.Config{Workers: 4, Policy: p})
		b := sim.Run(arrs, sim.Config{Workers: 4, Policy: p})
		if a.Utility != b.Utility || a.Completed != b.Completed {
			t.Errorf("%s not deterministic", p.Name())
		}
		if a.Completed+a.Shed != len(arrs) {
			t.Errorf("%s lost jobs: %d done + %d shed != %d", p.Name(), a.Completed, a.Shed, len(arrs))
		}
		if a.Utility > a.Max+1e-9 || a.OnTime+a.Late != a.Completed {
			t.Errorf("%s bad accounting: %+v", p.Name(), a)
		}
	}
}

// Regression guard: under overload, value-aware scheduling must clearly beat a
// plain queue on the default workload.
func TestOverloadWin(t *testing.T) {
	arrs := workload.Generate(1, 3000, 4, 1.3, workload.Default())
	fifo := sim.Run(arrs, sim.Config{Workers: 4, Policy: sched.FIFO{}})
	for _, p := range []sched.Policy{sched.Shed{Policy: sched.Density{}}, sched.Rollout{}} {
		r := sim.Run(arrs, sim.Config{Workers: 4, Policy: p})
		if r.Captured() < 3*fifo.Captured() {
			t.Errorf("%s captured %.1f%% vs fifo %.1f%%", p.Name(), 100*r.Captured(), 100*fifo.Captured())
		}
	}
}
