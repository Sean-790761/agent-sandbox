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

	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// RegisterWarmPoolCollector registers the custom Prometheus collector for warm pool gauges.
func RegisterWarmPoolCollector(c client.Client, logger logr.Logger) {
	collector := NewWarmPoolCollector(c, logger)
	if err := metrics.Registry.Register(collector); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			logger.Error(err, "Failed to register WarmPoolCollector")
		} else {
			logger.Info("WarmPoolCollector already registered, ignoring")
		}
	}
}

// WarmPoolCollector scrapes SandboxWarmPool replica gauges.
type WarmPoolCollector struct {
	client client.Client
	logger logr.Logger
}

// NewWarmPoolCollector initializes a WarmPoolCollector.
func NewWarmPoolCollector(c client.Client, logger logr.Logger) *WarmPoolCollector {
	return &WarmPoolCollector{client: c, logger: logger}
}

// Describe sends metric descriptors to the channel.
func (c *WarmPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- AgentSandboxWarmPoolDesiredReplicasDesc
	ch <- AgentSandboxWarmPoolReplicasDesc
	ch <- AgentSandboxWarmPoolReadyReplicasDesc
	ch <- AgentSandboxWarmPoolDeficitDesc
}

// Collect lists SandboxWarmPools and emits per-pool replica gauges.
func (c *WarmPoolCollector) Collect(ch chan<- prometheus.Metric) {
	var poolList extensionsv1beta1.SandboxWarmPoolList
	ctx, cancel := context.WithTimeout(context.Background(), metricsCollectTimeout)
	defer cancel()

	if err := c.client.List(ctx, &poolList, client.UnsafeDisableDeepCopy); err != nil {
		c.logger.Error(err, "Failed to list sandbox warm pools for metrics collection")
		return
	}

	for i := range poolList.Items {
		pool := &poolList.Items[i]
		template := UnknownTemplateSentinel
		if pool.Spec.TemplateRef.Name != "" {
			template = pool.Spec.TemplateRef.Name
		}
		desired := int32(1)
		if pool.Spec.Replicas != nil {
			desired = *pool.Spec.Replicas
		}
		replicas := pool.Status.Replicas
		ready := pool.Status.ReadyReplicas
		deficit := desired - ready
		if deficit < 0 {
			deficit = 0
		}
		labels := []string{pool.Namespace, pool.Name, template}
		ch <- prometheus.MustNewConstMetric(AgentSandboxWarmPoolDesiredReplicasDesc, prometheus.GaugeValue, float64(desired), labels...)
		ch <- prometheus.MustNewConstMetric(AgentSandboxWarmPoolReplicasDesc, prometheus.GaugeValue, float64(replicas), labels...)
		ch <- prometheus.MustNewConstMetric(AgentSandboxWarmPoolReadyReplicasDesc, prometheus.GaugeValue, float64(ready), labels...)
		ch <- prometheus.MustNewConstMetric(AgentSandboxWarmPoolDeficitDesc, prometheus.GaugeValue, float64(deficit), labels...)
	}
}
