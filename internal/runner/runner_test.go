package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/estimate"
	"github.com/Gauravkarthikeyan/kairos/internal/model"
	"github.com/Gauravkarthikeyan/kairos/internal/sched"
)

func TestRunsEverythingAndLearns(t *testing.T) {
	var calls atomic.Int32
	spec := &model.Spec{Workers: 2, Jobs: []model.JobSpec{{
		Name: "tick", Cmd: []string{"x"}, Every: model.Duration(20 * time.Millisecond),
		Deadline: model.Duration(time.Second), Hint: model.Duration(time.Millisecond),
		Curve: model.Curve{Kind: "step", Value: 5},
	}}}
	est := estimate.New(8, 0.7)
	r := &Runner{Spec: spec, Policy: sched.Shed{Policy: sched.Density{}}, Est: est,
		Exec: func(ctx context.Context, cmd []string) error {
			calls.Add(1)
			time.Sleep(5 * time.Millisecond)
			return nil
		}}
	s := r.Run(context.Background(), 100*time.Millisecond)
	if s.Completed != int(calls.Load()) || s.Completed < 5 || s.OnTime != s.Completed || s.Utility != float64(s.Completed)*5 {
		t.Fatalf("%+v calls=%d", s, calls.Load())
	}
	if got := est.Estimate("tick", 0); got < 4*time.Millisecond {
		t.Fatalf("estimator did not learn, got %v", got)
	}
}

func TestFailuresEarnNothing(t *testing.T) {
	spec := &model.Spec{Workers: 1, Jobs: []model.JobSpec{{
		Name: "bad", Cmd: []string{"x"}, Deadline: model.Duration(time.Second),
		Curve: model.Curve{Kind: "step", Value: 5},
	}}}
	r := &Runner{Spec: spec, Policy: sched.FIFO{}, Est: estimate.New(8, 0.7),
		Exec: func(context.Context, []string) error { return context.DeadlineExceeded }}
	s := r.Run(context.Background(), 0)
	if s.Failed != 1 || s.Utility != 0 {
		t.Fatalf("%+v", s)
	}
}
