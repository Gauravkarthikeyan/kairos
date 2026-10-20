// Package sim is a discrete-event simulator. It drives any sched.Policy on a
// virtual clock, so a day of scheduling takes milliseconds and the same seed
// always gives the same answer.
package sim

import (
	"sort"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/estimate"
	"github.com/Gauravkarthikeyan/kairos/internal/model"
	"github.com/Gauravkarthikeyan/kairos/internal/sched"
)

// Arrival is one job instance. True is how long it really runs; the policy
// never sees it, only what the estimator has learned from earlier runs.
type Arrival struct {
	Class     string
	Release   time.Duration
	Deadline  time.Duration // absolute
	True      time.Duration
	Hint      time.Duration
	Curve     model.Curve
	Droppable bool
}

type Config struct {
	Workers int
	Policy  sched.Policy
	Est     *estimate.Estimator // nil = fresh estimator
	Record  bool                // keep a per-job trace
}

type Event struct {
	Class      string
	Release    time.Duration
	Start, End time.Duration // zero for shed jobs
	Worker     int
	Utility    float64
	Shed       bool
}

type Result struct {
	Policy    string
	Utility   float64
	Max       float64
	Completed int
	OnTime    int
	Late      int
	Shed      int
	MeanLate  time.Duration // over completed jobs that were late
	Trace     []Event
}

// Captured is the share of all possible value the policy earned.
func (r Result) Captured() float64 {
	if r.Max == 0 {
		return 0
	}
	return r.Utility / r.Max
}

func Run(arrs []Arrival, cfg Config) Result {
	in := append([]Arrival(nil), arrs...)
	sort.SliceStable(in, func(i, j int) bool { return in[i].Release < in[j].Release })
	est := cfg.Est
	if est == nil {
		est = estimate.New(32, 0.7)
	}
	shedder, _ := cfg.Policy.(sched.Shedder)

	res := Result{Policy: cfg.Policy.Name()}
	free := make([]time.Duration, cfg.Workers) // when each worker next idles
	type running struct {
		end    time.Duration
		worker int
		task   *sched.Task
		start  time.Duration
	}
	var run []running
	var ready []*sched.Task
	next := 0
	var lateSum time.Duration

	for _, a := range in {
		res.Max += a.Curve.Value
	}

	var now time.Duration
	for next < len(in) || len(ready) > 0 || len(run) > 0 {
		// Jump to the next event: an arrival or a completion.
		t := time.Duration(1<<62 - 1)
		if next < len(in) {
			t = in[next].Release
		}
		for _, r := range run {
			t = min(t, r.end)
		}
		now = max(now, t)

		// Completions.
		kept := run[:0]
		for _, r := range run {
			if r.end > now {
				kept = append(kept, r)
				continue
			}
			a := in[r.task.ID]
			u := r.task.UtilityAt(r.end)
			res.Utility += u
			res.Completed++
			if r.end <= r.task.Deadline {
				res.OnTime++
			} else {
				res.Late++
				lateSum += r.end - r.task.Deadline
			}
			est.Observe(a.Class, a.True)
			if cfg.Record {
				res.Trace = append(res.Trace, Event{a.Class, a.Release, r.start, r.end, r.worker, u, false})
			}
		}
		run = kept

		// Arrivals.
		for next < len(in) && in[next].Release <= now {
			a := in[next]
			ready = append(ready, &sched.Task{
				ID: next, Class: a.Class, Release: a.Release, Deadline: a.Deadline,
				Hint: a.Hint, Curve: a.Curve, Droppable: a.Droppable,
			})
			next++
		}

		// Dispatch while a worker is idle and work is waiting.
		for len(ready) > 0 {
			w := idleWorker(free, now)
			if w < 0 {
				break
			}
			for _, tk := range ready {
				tk.Est = est.Estimate(tk.Class, tk.Hint)
			}
			if shedder != nil {
				live := ready[:0]
				for _, tk := range ready {
					if shedder.Hopeless(now, tk) {
						res.Shed++
						if cfg.Record {
							res.Trace = append(res.Trace, Event{Class: tk.Class, Release: tk.Release, Shed: true})
						}
						continue
					}
					live = append(live, tk)
				}
				ready = live
				if len(ready) == 0 {
					break
				}
			}
			view := make([]time.Duration, len(free))
			for i, f := range free {
				view[i] = max(f, now)
			}
			i := cfg.Policy.Pick(now, ready, view)
			tk := ready[i]
			ready = append(ready[:i], ready[i+1:]...)
			end := now + in[tk.ID].True
			free[w] = end
			run = append(run, running{end: end, worker: w, task: tk, start: now})
		}
	}

	if res.Late > 0 {
		res.MeanLate = lateSum / time.Duration(res.Late)
	}
	return res
}

func idleWorker(free []time.Duration, now time.Duration) int {
	for i, f := range free {
		if f <= now {
			return i
		}
	}
	return -1
}
