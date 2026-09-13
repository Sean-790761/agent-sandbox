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
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

func TestWarmPoolCollector(t *testing.T) {
	pools := []runtime.Object{
		&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "default"},
			Spec: extensionsv1beta1.SandboxWarmPoolSpec{
				Replicas:    ptr.To(int32(5)),
				TemplateRef: extensionsv1beta1.SandboxTemplateRef{Name: "tmpl-a"},
			},
			Status: extensionsv1beta1.SandboxWarmPoolStatus{
				Replicas:      4,
				ReadyReplicas: 2,
			},
		},
		&extensionsv1beta1.SandboxWarmPool{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-b", Namespace: "default"},
			Spec:       extensionsv1beta1.SandboxWarmPoolSpec{},
			Status: extensionsv1beta1.SandboxWarmPoolStatus{
				Replicas:      1,
				ReadyReplicas: 1,
			},
		},
	}

	collector := NewWarmPoolCollector(newExtensionsFakeClient(pools...).Build(), logr.Discard())
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))

	for _, name := range []string{
		"agent_sandbox_warmpool_desired_replicas",
		"agent_sandbox_warmpool_replicas",
		"agent_sandbox_warmpool_ready_replicas",
		"agent_sandbox_warmpool_deficit",
	} {
		count, err := testutil.GatherAndCount(registry, name)
		require.NoError(t, err, name)
		require.Equal(t, 2, count, name)
	}

	metricFamilies, err := registry.Gather()
	require.NoError(t, err)
	deficit := map[string]float64{}
	for _, mf := range metricFamilies {
		if mf.GetName() != "agent_sandbox_warmpool_deficit" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			deficit[labels["warmpool"]+"|"+labels["sandbox_template"]] = m.GetGauge().GetValue()
		}
	}
	require.Equal(t, map[string]float64{
		"pool-a|tmpl-a":                    3,
		"pool-b|" + UnknownTemplateSentinel: 0,
	}, deficit)
}
