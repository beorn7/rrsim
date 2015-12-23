package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	addr = flag.String(
		"addr", ":8080",
		"The address to bind to (for exposition of the /metric HTTP endpoint).",
	)
)

func waitDurationNs() float64 {
	return 1e9 * (rand.NormFloat64()**jitter + 1) / *qps
}

func runTask(id, batch int, duration time.Duration) {
	log.Printf("Starting task %d of batch %d.\n", id, batch)
	defer log.Printf("Stopping task %d of batch %d.\n", id, batch)

	cnt := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "queries_total",
		Help: "Number of (simulated) queries the task has served.",
		ConstLabels: prometheus.Labels{
			"batch": fmt.Sprint(batch),
			"task":  fmt.Sprint(id),
		},
	})
	prometheus.MustRegister(cnt)
	defer prometheus.Unregister(cnt)

	stopTimer := time.NewTimer(duration)
	queryTimer := time.NewTimer(time.Duration(waitDurationNs() * rand.Float64()))
	for {
		select {
		case <-stopTimer.C:
			return
		case <-queryTimer.C:
			cnt.Inc()
			queryTimer.Reset(time.Duration(waitDurationNs()))
		}
	}
}

func main() {
	flag.Parse()

	http.Handle("/metrics", prometheus.Handler())
	go http.ListenAndServe(*addr, nil)

	batch := 0

	// First start one batch of already running tasks.
	for i := 0; i < *num; i++ {
		go runTask(i, batch, *runDuration+*restartDuration*time.Duration(i)/time.Duration(*num))
	}

	for {
		time.Sleep(*runDuration)
		batch++
		log.Printf("Initiating restart batch %d.\n", batch)
		for i := 0; i < *num; i++ {
			go runTask(i, batch, *runDuration+*restartDuration)
			time.Sleep(*restartDuration / time.Duration(*num))
		}
		log.Printf("Restart batch %d complete.\n", batch)
	}
}
