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

// nolint:revive
package metrics

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// AgentSandboxClaimsMetricKey aggregates identical claim inventory label combinations.
type AgentSandboxClaimsMetricKey struct {
	Namespace      string
	ReadyCondition string
	Reason         string
}

// NewAgentSandboxClaimsConstMetric creates a ConstMetric for agent_sandbox_claims.
func NewAgentSandboxClaimsConstMetric(count int, key AgentSandboxClaimsMetricKey) prometheus.Metric {
	return prometheus.MustNewConstMetric(
		AgentSandboxClaimsDesc,
		prometheus.GaugeValue,
		float64(count),
		key.Namespace,
		key.ReadyCondition,
		key.Reason,
	)
}

// RegisterClaimCollector registers the custom Prometheus collector for claim counts.
func RegisterClaimCollector(c client.Client, logger logr.Logger) {
	collector := NewClaimCollector(c, logger)
	if err := metrics.Registry.Register(collector); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			logger.Error(err, "Failed to register ClaimCollector")
		} else {
			logger.Info("ClaimCollector already registered, ignoring")
		}
	}
}

// ClaimCollector dynamically scrapes SandboxClaim inventory counts.
type ClaimCollector struct {
	client client.Client
	logger logr.Logger
	desc   *prometheus.Desc
}

// NewClaimCollector initializes a ClaimCollector.
func NewClaimCollector(c client.Client, logger logr.Logger) *ClaimCollector {
	return &ClaimCollector{
		client: c,
		logger: logger,
		desc:   AgentSandboxClaimsDesc,
	}
}

// Describe sends the metric descriptor to the channel.
func (c *ClaimCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect lists SandboxClaims and emits aggregated inventory gauges.
func (c *ClaimCollector) Collect(ch chan<- prometheus.Metric) {
	var claimList extensionsv1beta1.SandboxClaimList
	ctx, cancel := context.WithTimeout(context.Background(), metricsCollectTimeout)
	defer cancel()

	if err := c.client.List(ctx, &claimList, client.UnsafeDisableDeepCopy); err != nil {
		c.logger.Error(err, "Failed to list sandbox claims for metrics collection")
		return
	}

	counts := make(map[AgentSandboxClaimsMetricKey]int)
	for i := range claimList.Items {
		claim := &claimList.Items[i]
		readyConditionStr := "false"
		reason := "None"
		readyCond := meta.FindStatusCondition(claim.Status.Conditions, string(sandboxv1beta1.SandboxConditionReady))
		if readyCond != nil {
			if readyCond.Status == metav1.ConditionTrue {
				readyConditionStr = "true"
				reason = "None"
			} else {
				reason = NormalizeClaimErrorReason(readyCond.Reason)
			}
		}
		key := AgentSandboxClaimsMetricKey{
			Namespace:      claim.Namespace,
			ReadyCondition: readyConditionStr,
			Reason:         reason,
		}
		counts[key]++
	}

	for key, count := range counts {
		ch <- NewAgentSandboxClaimsConstMetric(count, key)
	}
}
