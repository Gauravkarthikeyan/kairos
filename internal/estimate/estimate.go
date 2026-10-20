// Package estimate learns how long each class of job takes from its own history.
package estimate

import (
	"math"
	"sort"
	"sync"
	"time"
)

const minSamples = 3

// Estimator keeps a sliding window of observed durations per job class and
// answers with a quantile of that window. A quantile above the median makes
// the scheduler a little pessimistic, which is the safe direction for
// deadlines.
type Estimator struct {
	mu      sync.Mutex
	window  int
	q       float64
	samples map[string][]time.Duration
}

func New(window int, q float64) *Estimator {
	if window < minSamples {
		window = minSamples
	}
	return &Estimator{window: window, q: q, samples: map[string][]time.Duration{}}
}

func (e *Estimator) Observe(class string, d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := append(e.samples[class], d)
	if len(s) > e.window {
		s = s[len(s)-e.window:]
	}
	e.samples[class] = s
}

// Estimate returns the learned duration for class, or hint until there are
// enough samples to trust.
func (e *Estimator) Estimate(class string, hint time.Duration) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.samples[class]
	if len(s) < minSamples {
		return hint
	}
	c := append([]time.Duration(nil), s...)
	sort.Slice(c, func(i, j int) bool { return c[i] < c[j] })
	idx := int(math.Ceil(e.q*float64(len(c)))) - 1
	if idx < 0 {
		idx = 0
	}
	return c[idx]
}
