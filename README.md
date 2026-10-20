# Kairos

**A job scheduler for when you have more work than capacity.**

Cron, Celery, Sidekiq and most batch queues run jobs in the order they arrived. That is fine until the system is overloaded, and then it fails in a specific, expensive way: the queue fills with work that is already late and no longer matters, and it blocks the work that still does. A page alert that must run within seconds waits behind a thousand thumbnails.

Kairos asks each job one extra question: **what does it cost if you finish me late?** You answer with a small curve, and the scheduler maximizes the total value earned instead of the order of arrival.

```json
{
  "name": "page-alert",
  "cmd": ["./notify", "--oncall"],
  "every": "30s",
  "deadline": "10s",
  "curve": { "kind": "step", "value": 100 },
  "droppable": true
}
```

| curve | meaning | typical job |
|---|---|---|
| `step` | full value on time, nothing after | alerts, SLA-bound calls |
| `linear` | value fades to zero over `grace` | reports, reconciliation |
| `exp` | value decays with time constant `grace` | cache warming, thumbnails |
| `floor` | fraction of value kept however late | backups that must eventually run |

`droppable` jobs are shed once they can no longer earn anything, so capacity goes to work that still matters.

## Results

Measured by the deterministic simulator in this repo: 5,000 jobs, 4 workers, six job classes (alerts, reconciliation, backups, reports, cache warming, thumbnails), seed 1. The table shows the share of all possible value each policy captured as offered load rises past capacity.

| load | fifo | edf | edf+shed | density+shed | rollout |
|---|---|---|---|---|---|
| 0.6x | 96.0% | 98.7% | 98.9% | 99.0% | 99.2% |
| 0.8x | 85.4% | 98.0% | 97.9% | 97.1% | 98.5% |
| 1.0x | 12.8% | 15.9% | 60.9% | 94.2% | 90.2% |
| 1.2x | 10.3% | 12.0% | 19.7% | 88.0% | 85.8% |
| 1.4x | 9.1% | 11.2% | 14.0% | 83.6% | 83.3% |
| 1.6x | 8.3% | 10.1% | 11.0% | 80.4% | 81.2% |
| 1.8x | 8.1% | 9.5% | 9.9% | 81.4% | 76.9% |

What this shows, and what it does not:

- Once load reaches capacity, arrival-order (FIFO) and deadline-order (EDF) scheduling collapse to about 10% of the value. Value-aware scheduling holds 80-94%.
- The win comes from two things together: knowing what each job is worth, and shedding jobs that are already hopeless. Earliest-deadline-first with shedding alone is not enough.
- The one-step-lookahead `rollout` policy ties the simpler `density+shed` policy and sometimes loses to it under heavy overload, so `density` is the default. Rollout is kept because it is slightly better at light load and is a useful reference.
- These are synthetic workloads with made-up value curves. They show the mechanism works; your numbers depend on your curves.

Reproduce: `go run ./cmd/kairos simulate -sweep`

## Install and use

```
go install github.com/Gauravkarthikeyan/kairos/cmd/kairos@latest
```

**Plan** what an overloaded spec will lose before you deploy it:

```
$ kairos plan -spec examples/ops.json -horizon 20s
84 job instances over 20s on 2 workers, assuming every job takes exactly its hint

policy        value captured  on time  late  shed
fifo           99.4%          55       29    0
density+shed   99.7%          81       2     1

jobs predicted to miss their deadline or be shed (value lost):
  thumbnail  2 instances  -5
```

**Run** the commands for real:

```
$ kairos run -spec examples/ops.json -for 30s
[   6.46s] done   page-alert#26 took=305ms utility=100.0/100
[   6.46s] start  thumbnail#25 est=205ms waited=456ms
...
density+shed: value captured 99.9% (1058 of 1059) | completed 28 | shed 0 | failed 0
```

**Simulate** a synthetic overload and compare every policy:

```
kairos simulate -load 1.3 -workers 4
kairos simulate -sweep
```

## How it works

- **Learned durations.** Each job class keeps a sliding window of its recent run times and the scheduler uses the 70th percentile, falling back to your `hint` until it has three samples. Slightly pessimistic estimates are the safe direction for deadlines.
- **Policies** (`internal/sched`) are pure functions of the clock, the ready queue and when each worker frees up. The same code runs under the simulator's virtual clock and the real runner's wall clock, so what you simulate is what you run.
- **density** starts the job with the most remaining value per second of work. **rollout** plays the rest of the queue forward for each candidate and starts the one with the best total.
- **Shedding** drops a droppable job once the best it could still earn is under 1% of its value.
- **Simulator** (`internal/sim`) is a discrete-event engine: a day of scheduling runs in milliseconds and the same seed always gives the same result. Jobs have hidden true durations, so policies must rely on what they have learned.

## Limits

- Single process and single machine; there is no persistence or distributed coordination yet.
- Jobs are independent. There are no dependency graphs.
- Value curves are only as good as the numbers you put in them.
- Time-utility scheduling is established in real-time systems research. What Kairos adds is packaging it as a cron-style tool with a planner and a reproducible simulator, not a new theory.

## Layout

```
cmd/kairos        CLI: simulate, plan, run
internal/model    curves, durations, spec format
internal/sched    fifo, edf, density, rollout, shedding
internal/estimate learned duration estimates
internal/sim      deterministic discrete-event simulator
internal/workload reproducible synthetic workloads
internal/runner   real command execution
examples/         sample spec
```

```
go test -race ./...
```

## License

MIT
