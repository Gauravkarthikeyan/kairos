// Package sched contains the dispatch policies. A policy is a pure function of
// the current time, the ready queue and when each worker frees up, which is
// what lets the same code run under the simulator and the real runner.
package sched

import (
	"sort"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/model"
)

// ShedFloor is the fraction of a job's value below which a droppable job is
// considered not worth running any more.
const ShedFloor = 0.01

type Task struct {
	ID        int
	Class     string
	Release   time.Duration
	Deadline  time.Duration // absolute
	Hint      time.Duration
	Est       time.Duration // refreshed from history before each decision
	Curve     model.Curve
	Droppable bool
}

// UtilityAt is the value earned if the task finishes at the given time.
func (t *Task) UtilityAt(finish time.Duration) float64 {
	return t.Curve.At(finish - t.Deadline)
}

type Policy interface {
	Name() string
	// Pick returns the index in ready of the task to start now. free[i] is the
	// time worker i becomes idle; at least one entry equals now.
	Pick(now time.Duration, ready []*Task, free []time.Duration) int
}

// Shedder is implemented by policies that drop work which can no longer earn
// anything worthwhile.
type Shedder interface {
	Hopeless(now time.Duration, t *Task) bool
}

func hopeless(now time.Duration, t *Task) bool {
	return t.Droppable && t.UtilityAt(now+t.Est) < ShedFloor*t.Curve.Value
}

// FIFO runs jobs in release order, like a classic cron queue.
type FIFO struct{}

func (FIFO) Name() string { return "fifo" }
func (FIFO) Pick(_ time.Duration, ready []*Task, _ []time.Duration) int {
	return argmin(ready, func(t *Task) float64 { return float64(t.Release)*1e3 + float64(t.ID)*1e-3 })
}

// EDF runs the earliest absolute deadline first.
type EDF struct{}

func (EDF) Name() string { return "edf" }
func (EDF) Pick(_ time.Duration, ready []*Task, _ []time.Duration) int {
	return argmin(ready, func(t *Task) float64 { return float64(t.Deadline) })
}

// Density runs the job with the most value per second of work.
type Density struct{}

func (Density) Name() string { return "density" }
func (Density) Pick(now time.Duration, ready []*Task, _ []time.Duration) int {
	return argmin(ready, func(t *Task) float64 {
		return -t.UtilityAt(now+t.Est) / float64(max(t.Est, time.Millisecond))
	})
}

// Shed wraps a policy so it also drops hopeless droppable jobs. It exists so
// the baselines can be compared fairly against Rollout, which always sheds.
type Shed struct{ Policy }

func (s Shed) Name() string                           { return s.Policy.Name() + "+shed" }
func (Shed) Hopeless(now time.Duration, t *Task) bool { return hopeless(now, t) }

// Rollout is one-step lookahead. For every plausible candidate it plays the
// rest of the queue forward over the workers using value-density order,
// adds up the utility that schedule would earn, and starts the candidate with
// the best total. Because the future is scored in the jobs' own value curves,
// it will happily let a cheap job go late to save an expensive one.
type Rollout struct {
	K       int // candidates examined per decision (default 6)
	Horizon int // jobs played forward per candidate (default 48)
}

func (Rollout) Name() string                             { return "rollout" }
func (Rollout) Hopeless(now time.Duration, t *Task) bool { return hopeless(now, t) }

func (r Rollout) Pick(now time.Duration, ready []*Task, free []time.Duration) int {
	if len(ready) == 1 {
		return 0
	}
	k, h := r.K, r.Horizon
	if k <= 0 {
		k = 6
	}
	if h <= 0 {
		h = 48
	}
	byDeadline := order(ready, func(t *Task) float64 { return float64(t.Deadline) })
	byDensity := order(ready, func(t *Task) float64 {
		return -t.UtilityAt(now+t.Est) / float64(max(t.Est, time.Millisecond))
	})

	// Candidates: the k earliest deadlines plus the k best value densities,
	// examined in deadline order so ties favour the more urgent job.
	cand := map[int]bool{}
	for i := 0; i < k && i < len(byDeadline); i++ {
		cand[byDeadline[i]] = true
		cand[byDensity[i]] = true
	}

	best, bestScore := -1, -1.0
	for _, c := range byDeadline {
		if !cand[c] {
			continue
		}
		if s := rollout(now, ready, byDensity, c, free, h); s > bestScore {
			best, bestScore = c, s
		}
	}
	return best
}

// rollout scores the schedule that starts ready[first] now and then runs the
// other jobs in `order` on whichever worker frees up first.
func rollout(now time.Duration, ready []*Task, order []int, first int, free []time.Duration, horizon int) float64 {
	w := make([]time.Duration, len(free))
	for i, f := range free {
		w[i] = max(f, now)
	}
	total := place(w, ready[first])
	played := 0
	for _, i := range order {
		if i == first {
			continue
		}
		if played == horizon {
			break
		}
		played++
		t := ready[i]
		j := earliest(w)
		if t.Droppable && t.UtilityAt(w[j]+t.Est) < ShedFloor*t.Curve.Value {
			continue // it would be shed, so it costs no capacity
		}
		total += place(w, t)
	}
	return total
}

func place(w []time.Duration, t *Task) float64 {
	j := earliest(w)
	w[j] += t.Est
	return t.UtilityAt(w[j])
}

func earliest(w []time.Duration) int {
	j := 0
	for i := range w {
		if w[i] < w[j] {
			j = i
		}
	}
	return j
}

func argmin(ready []*Task, key func(*Task) float64) int {
	best := 0
	for i := 1; i < len(ready); i++ {
		if key(ready[i]) < key(ready[best]) {
			best = i
		}
	}
	return best
}

func order(ready []*Task, key func(*Task) float64) []int {
	idx := make([]int, len(ready))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return key(ready[idx[a]]) < key(ready[idx[b]]) })
	return idx
}
