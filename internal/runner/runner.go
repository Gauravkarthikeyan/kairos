// Package runner executes real commands under a sched.Policy on the wall clock.
package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/estimate"
	"github.com/Gauravkarthikeyan/kairos/internal/model"
	"github.com/Gauravkarthikeyan/kairos/internal/sched"
)

// Exec runs one command to completion. It is a field so tests can fake it.
type Exec func(ctx context.Context, cmd []string) error

func OSExec(ctx context.Context, cmd []string) error {
	return exec.CommandContext(ctx, cmd[0], cmd[1:]...).Run()
}

type Summary struct {
	Utility, Max float64
	Completed    int
	OnTime, Late int
	Shed, Failed int
}

type Runner struct {
	Spec   *model.Spec
	Policy sched.Policy
	Est    *estimate.Estimator
	Exec   Exec
	Log    io.Writer
}

// Run releases jobs on their schedules for `horizon`, dispatches them, and
// returns once everything released has finished (or ctx is cancelled).
func (r *Runner) Run(ctx context.Context, horizon time.Duration) Summary {
	start := time.Now()
	since := func() time.Duration { return time.Since(start) }
	shedder, _ := r.Policy.(sched.Shedder)

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		sum      Summary
		ready    []*sched.Task
		busy     = make([]bool, r.Spec.Workers)
		estEnd   = make([]time.Duration, r.Spec.Workers)
		pending  int
		nextID   int
		wake     = make(chan struct{}, 1)
		specByID = map[string]model.JobSpec{}
	)
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	logf := func(format string, a ...any) {
		if r.Log != nil {
			fmt.Fprintf(r.Log, "[%7.2fs] %s\n", since().Seconds(), fmt.Sprintf(format, a...))
		}
	}

	var timers []*time.Timer
	for _, j := range r.Spec.Jobs {
		specByID[j.Name] = j
		for at := time.Duration(0); at <= horizon; at += j.Every.D() {
			j, at := j, at
			mu.Lock()
			pending++
			mu.Unlock()
			timers = append(timers, time.AfterFunc(max(0, at-since()), func() {
				mu.Lock()
				rel := since()
				ready = append(ready, &sched.Task{
					ID: nextID, Class: j.Name, Release: rel, Deadline: rel + j.Deadline.D(),
					Hint: j.Hint.D(), Curve: j.Curve, Droppable: j.Droppable,
				})
				nextID++
				sum.Max += j.Curve.Value
				pending--
				mu.Unlock()
				signal()
			}))
			if j.Every == 0 {
				break
			}
		}
	}
	defer func() {
		for _, t := range timers {
			t.Stop()
		}
	}()

	for {
		mu.Lock()
		for len(ready) > 0 {
			slot := -1
			for i := range busy {
				if !busy[i] {
					slot = i
					break
				}
			}
			if slot < 0 {
				break
			}
			now := since()
			for _, t := range ready {
				t.Est = r.Est.Estimate(t.Class, t.Hint)
			}
			if shedder != nil {
				live := ready[:0]
				for _, t := range ready {
					if shedder.Hopeless(now, t) {
						sum.Shed++
						logf("shed   %s#%d (can no longer earn its keep)", t.Class, t.ID)
						continue
					}
					live = append(live, t)
				}
				ready = live
				if len(ready) == 0 {
					break
				}
			}
			free := make([]time.Duration, len(busy))
			for i := range free {
				free[i] = now
				if busy[i] {
					free[i] = max(now, estEnd[i])
				}
			}
			i := r.Policy.Pick(now, ready, free)
			t := ready[i]
			ready = append(ready[:i], ready[i+1:]...)
			busy[slot] = true
			estEnd[slot] = now + t.Est
			job := specByID[t.Class]
			logf("start  %s#%d est=%s waited=%s", t.Class, t.ID, t.Est.Round(time.Millisecond), (now - t.Release).Round(time.Millisecond))

			wg.Add(1)
			go func(slot int, t *sched.Task) {
				defer wg.Done()
				cctx, cancel := ctx, context.CancelFunc(func() {})
				if job.Timeout > 0 {
					cctx, cancel = context.WithTimeout(ctx, job.Timeout.D())
				}
				began := since()
				err := r.Exec(cctx, job.Cmd)
				cancel()
				end := since()

				mu.Lock()
				busy[slot] = false
				if err != nil {
					sum.Failed++
					logf("fail   %s#%d: %v", t.Class, t.ID, err)
				} else {
					u := t.UtilityAt(end)
					sum.Utility += u
					sum.Completed++
					if end <= t.Deadline {
						sum.OnTime++
					} else {
						sum.Late++
					}
					r.Est.Observe(t.Class, end-began)
					logf("done   %s#%d took=%s utility=%.1f/%.0f", t.Class, t.ID, (end - began).Round(time.Millisecond), u, t.Curve.Value)
				}
				mu.Unlock()
				signal()
			}(slot, t)
		}
		running := 0
		for _, b := range busy {
			if b {
				running++
			}
		}
		done := pending == 0 && len(ready) == 0 && running == 0
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-wake:
		case <-ctx.Done():
			wg.Wait()
			return sum
		}
	}
	wg.Wait()
	return sum
}
