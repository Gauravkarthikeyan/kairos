// Package model holds the types shared by every other package: durations that
// read nicely in JSON, time-utility curves, and the job spec file format.
package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"
)

// Duration is a time.Duration that unmarshals from "1m30s" or from a number
// of seconds.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch x := v.(type) {
	case string:
		td, err := time.ParseDuration(x)
		if err != nil {
			return err
		}
		*d = Duration(td)
	case float64:
		*d = Duration(x * float64(time.Second))
	default:
		return fmt.Errorf("duration must be a string or a number of seconds, got %T", v)
	}
	return nil
}

// Curve says how much a job is worth as a function of how late it finishes.
// Lateness <= 0 earns the full Value. After that:
//
//	step    drops to Floor*Value immediately
//	linear  falls linearly to Floor*Value over Grace
//	exp     decays toward Floor*Value with time constant Grace
type Curve struct {
	Kind  string   `json:"kind"`
	Value float64  `json:"value"`
	Grace Duration `json:"grace,omitempty"`
	// Floor is the fraction of Value still earned no matter how late the job
	// finishes. Use it for work that must eventually run.
	Floor float64 `json:"floor,omitempty"`
}

func (c Curve) Validate() error {
	if c.Value <= 0 {
		return errors.New("curve value must be > 0")
	}
	if c.Floor < 0 || c.Floor >= 1 {
		return errors.New("curve floor must be in [0, 1)")
	}
	switch c.Kind {
	case "step":
	case "linear", "exp":
		if c.Grace <= 0 {
			return fmt.Errorf("%s curve needs grace > 0", c.Kind)
		}
	default:
		return fmt.Errorf("unknown curve kind %q (want step, linear or exp)", c.Kind)
	}
	return nil
}

// At returns the utility earned when the job finishes `late` after its deadline.
func (c Curve) At(late time.Duration) float64 {
	if late <= 0 {
		return c.Value
	}
	floor := c.Floor * c.Value
	var v float64
	switch c.Kind {
	case "step":
		v = 0
	case "linear":
		g := float64(c.Grace)
		v = c.Value * math.Max(0, 1-float64(late)/g)
	case "exp":
		v = c.Value * math.Exp(-float64(late)/float64(c.Grace))
	}
	return math.Max(v, floor)
}

// JobSpec describes one kind of job in a spec file.
type JobSpec struct {
	Name      string   `json:"name"`
	Cmd       []string `json:"cmd"`
	Every     Duration `json:"every,omitempty"` // 0 = run once at t=0
	Deadline  Duration `json:"deadline"`        // relative to each release
	Curve     Curve    `json:"curve"`
	Hint      Duration `json:"hint,omitempty"` // duration guess until history exists
	Droppable bool     `json:"droppable,omitempty"`
	Timeout   Duration `json:"timeout,omitempty"` // kill the command after this long
}

type Spec struct {
	Workers int       `json:"workers"`
	Jobs    []JobSpec `json:"jobs"`
}

func (s *Spec) Validate() error {
	if s.Workers < 1 {
		return errors.New("workers must be >= 1")
	}
	if len(s.Jobs) == 0 {
		return errors.New("spec has no jobs")
	}
	seen := map[string]bool{}
	for i, j := range s.Jobs {
		if j.Name == "" {
			return fmt.Errorf("job %d has no name", i)
		}
		if seen[j.Name] {
			return fmt.Errorf("duplicate job name %q", j.Name)
		}
		seen[j.Name] = true
		if j.Deadline <= 0 {
			return fmt.Errorf("job %q needs a deadline > 0", j.Name)
		}
		if j.Every < 0 || j.Hint < 0 || j.Timeout < 0 {
			return fmt.Errorf("job %q has a negative duration", j.Name)
		}
		if err := j.Curve.Validate(); err != nil {
			return fmt.Errorf("job %q: %w", j.Name, err)
		}
	}
	return nil
}

func LoadSpec(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Spec
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}
