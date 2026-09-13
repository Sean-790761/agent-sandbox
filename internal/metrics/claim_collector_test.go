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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	sandboxv1beta1 "sigs.k8s.io/agent-sandbox/api/v1beta1"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

func newExtensionsFakeClient(objects ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = sandboxv1beta1.AddToScheme(scheme)
	_ = extensionsv1beta1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...)
}

func TestClaimCollector(t *testing.T) {
	claims := []runtime.Object{
		&extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "ready-claim", Namespace: "default"},
			Status: extensionsv1beta1.SandboxClaimStatus{
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionTrue,
					Reason: "SandboxReady",
				}},
			},
		},
		&extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "missing-template", Namespace: "default"},
			Status: extensionsv1beta1.SandboxClaimStatus{
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionFalse,
					Reason: "TemplateNotFound",
				}},
			},
		},
		&extensionsv1beta1.SandboxClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "unknown-reason", Namespace: "ns-b"},
			Status: extensionsv1beta1.SandboxClaimStatus{
				Conditions: []metav1.Condition{{
					Type:   string(sandboxv1beta1.SandboxConditionReady),
					Status: metav1.ConditionFalse,
					Reason: "SomethingCustom",
				}},
			},
		},
	}

	collector := NewClaimCollector(newExtensionsFakeClient(claims...).Build(), logr.Discard())
	registry := prometheus.NewRegistry()
	require.NoError(t, registry.Register(collector))

	count, err := testutil.GatherAndCount(registry, "agent_sandbox_claims")
	require.NoError(t, err)
	require.Equal(t, 3, count)

	metricFamilies, err := registry.Gather()
	require.NoError(t, err)
	got := map[string]float64{}
	for _, mf := range metricFamilies {
		if mf.GetName() != "agent_sandbox_claims" {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			key := labels["namespace"] + "|" + labels["ready_condition"] + "|" + labels["reason"]
			got[key] = m.GetGauge().GetValue()
		}
	}
	require.Equal(t, map[string]float64{
		"default|true|None":               1,
		"default|false|TemplateNotFound":  1,
		"ns-b|false|ReconcilerError":      1,
	}, got)
}
