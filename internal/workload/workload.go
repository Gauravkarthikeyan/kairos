// Package workload generates reproducible synthetic job streams for the
// simulator.
package workload

import (
	"math"
	"math/rand"
	"time"

	"github.com/Gauravkarthikeyan/kairos/internal/model"
	"github.com/Gauravkarthikeyan/kairos/internal/sim"
)

// Class is one family of jobs: how often it shows up, how long it runs
// (lognormal around Median), how tight its deadline is, and what lateness costs.
type Class struct {
	Name      string
	Weight    float64
	Median    time.Duration
	Sigma     float64
	Slack     float64 // deadline = release + Slack*Median
	Kind      string
	Value     float64
	GraceX    float64 // curve grace = GraceX*Median
	Floor     float64
	Droppable bool
}

func Default() []Class {
	s := time.Second
	return []Class{
		{"page-alert", 0.15, 2 * s, 0.3, 5, "step", 100, 0, 0, true},
		{"checkout-reconcile", 0.20, 8 * s, 0.5, 6, "linear", 60, 8, 0.1, false},
		{"nightly-backup", 0.05, 40 * s, 0.4, 5, "step", 80, 0, 0.3, false},
		{"report-render", 0.20, 12 * s, 0.6, 5, "linear", 25, 6, 0, true},
		{"cache-warm", 0.20, 3 * s, 0.5, 3, "exp", 8, 2, 0, true},
		{"thumbnail", 0.20, 1 * s, 0.7, 4, "exp", 3, 3, 0, true},
	}
}

func (c Class) curve() model.Curve {
	return model.Curve{
		Kind: c.Kind, Value: c.Value, Floor: c.Floor,
		Grace: model.Duration(c.GraceX * float64(c.Median)),
	}
}

// Generate returns n arrivals from a Poisson process whose rate is chosen so
// the offered load equals `load` times the capacity of `workers` workers.
// load < 1 is a comfortable system, load > 1 is overload.
func Generate(seed int64, n, workers int, load float64, classes []Class) []sim.Arrival {
	rng := rand.New(rand.NewSource(seed))
	var wsum, meanWork float64
	for _, c := range classes {
		wsum += c.Weight
		meanWork += c.Weight * c.Median.Seconds() * math.Exp(c.Sigma*c.Sigma/2)
	}
	meanWork /= wsum
	rate := load * float64(workers) / meanWork // arrivals per second

	out := make([]sim.Arrival, 0, n)
	var t float64
	for i := 0; i < n; i++ {
		t += rng.ExpFloat64() / rate
		c := pick(rng, classes, wsum)
		d := c.Median.Seconds() * math.Exp(c.Sigma*rng.NormFloat64())
		d = math.Min(math.Max(d, 0.001), 20*c.Median.Seconds())
		rel := time.Duration(t * float64(time.Second))
		out = append(out, sim.Arrival{
			Class:     c.Name,
			Release:   rel,
			Deadline:  rel + time.Duration(c.Slack*float64(c.Median)),
			True:      time.Duration(d * float64(time.Second)),
			Hint:      c.Median,
			Curve:     c.curve(),
			Droppable: c.Droppable,
		})
	}
	return out
}

func pick(rng *rand.Rand, classes []Class, wsum float64) Class {
	x := rng.Float64() * wsum
	for _, c := range classes {
		if x < c.Weight {
			return c
		}
		x -= c.Weight
	}
	return classes[len(classes)-1]
}
