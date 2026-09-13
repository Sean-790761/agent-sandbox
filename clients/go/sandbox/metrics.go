// Copyright 2026 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sandbox

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	latencyBucketsMS = []float64{100, 250, 500, 750, 1000, 1250, 1500, 2000, 2500, 5000, 10000, 30000, 60000, 120000, 240000}

	// SandboxClientCreateLatencyMS measures CreateSandbox end-to-end latency
	// (claim create through Open/ready) in milliseconds.
	SandboxClientCreateLatencyMS = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sandbox_client_create_latency_ms",
			Help:    "Latency of creating a sandbox via the Go client (claim create through ready) in milliseconds.",
			Buckets: latencyBucketsMS,
		},
		[]string{"status"},
	)

	// SandboxClientDiscoveryLatencyMS measures reconnect/discovery latency in milliseconds.
	SandboxClientDiscoveryLatencyMS = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sandbox_client_discovery_latency_ms",
			Help:    "Latency of discovering/reattaching to an existing sandbox via the Go client in milliseconds.",
			Buckets: latencyBucketsMS,
		},
		[]string{"mode", "status"},
	)
)

func init() {
	prometheus.MustRegister(SandboxClientCreateLatencyMS)
	prometheus.MustRegister(SandboxClientDiscoveryLatencyMS)
}

func observeCreateLatency(start time.Time, err error) {
	status := "success"
	if err != nil {
		status = "failure"
	}
	SandboxClientCreateLatencyMS.WithLabelValues(status).Observe(float64(time.Since(start).Milliseconds()))
}

func observeDiscoveryLatency(start time.Time, mode string, err error) {
	status := "success"
	if err != nil {
		status = "failure"
	}
	SandboxClientDiscoveryLatencyMS.WithLabelValues(mode, status).Observe(float64(time.Since(start).Milliseconds()))
}
