package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	num = flag.Int(
		"n", 20,
		"Number of tasks per batch.",
	)
	restartDuration = flag.Duration(
		"restart-duration", time.Minute,
		"Duration of a rolling restart.",
	)
	runDuration = flag.Duration(
		"run-duration", time.Minute,
		"Duration between restarts (and initial time before the first restart).",
	)
	qps = flag.Float64(
		"qps", 10,
		"Average queries per second per task.",
	)
	jitter = flag.Float64(
		"jitter", 0,
		"How much the wait time between queries is randomly changed. The wait time between queries is normal-distributed with the given jitter value equaling σ/μ.",
	)
	loss = flag.Float64(
		"loss", 0,
		"Relative amount of lost scrapes. This is simulated by removing a counter from the exposed metrics for 1s now and then.",
	)
	addr = flag.String(
		"addr", ":8080",
		"The address to bind to (for exposition of the /metric HTTP endpoint).",
	)
	enableOpenMetrics = flag.Bool(
		"enable-openmetrics", false,
		"Enable OpenMetrics encoding in the /metrics endpoint.",
	)
	enableOpenMetricsCreated = flag.Bool(
		"enable-openmetrics-created", false,
		"Enable _created suffix timestamps in OpenMetrics output.",
	)
)

func waitDurationNs() float64 {
	return 1e9 * (rand.NormFloat64()**jitter + 1) / *qps
}

func runTask(id, batch int, duration time.Duration, reg prometheus.Registerer) {
	log.Printf("Starting task %d of batch %d.\n", id, batch)
	defer log.Printf("Stopping task %d of batch %d.\n", id, batch)

	cnt := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "queries_total",
		Help: "Number of (simulated) queries the task has served.",
		ConstLabels: prometheus.Labels{
			"batch": fmt.Sprint(batch),
		},
	})
	reg.MustRegister(cnt)
	defer reg.Unregister(cnt)
	registered := true

	stopTimer := time.NewTimer(duration)
	queryTimer := time.NewTimer(time.Duration(waitDurationNs() * rand.Float64()))
	lossTicker := time.NewTicker(time.Second)
	defer lossTicker.Stop()

	for {
		select {
		case <-stopTimer.C:
			return
		case <-queryTimer.C:
			cnt.Inc()
			queryTimer.Reset(time.Duration(waitDurationNs()))
		case <-lossTicker.C:
			if rand.Float64() < *loss && registered {
				reg.Unregister(cnt)
				registered = false
			} else if !registered {
				reg.MustRegister(cnt)
				registered = true
			}
		}
	}
}

func main() {
	flag.Parse()

	regs := make([]*prometheus.Registry, *num)
	for i := range regs {
		regs[i] = prometheus.NewRegistry()
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(
			regs[i],
			promhttp.HandlerOpts{
				EnableOpenMetrics:                   *enableOpenMetrics,
				EnableOpenMetricsTextCreatedSamples: *enableOpenMetricsCreated,
			},
		))
		addr := fmt.Sprintf(":8%03d", i+80) // TODO: Ugly! Make configurabe through CLI.
		go http.ListenAndServe(addr, mux)
	}

	batch := 0

	// First start one batch of already running tasks.
	for i, reg := range regs {
		go runTask(
			i, batch,
			*runDuration+*restartDuration*time.Duration(i)/time.Duration(*num),
			reg,
		)
	}

	for {
		time.Sleep(*runDuration)
		batch++
		log.Printf("Initiating restart batch %d.\n", batch)
		for i, reg := range regs {
			go runTask(i, batch, *runDuration+*restartDuration, reg)
			time.Sleep(*restartDuration / time.Duration(*num))
		}
		log.Printf("Restart batch %d complete.\n", batch)
	}
}
