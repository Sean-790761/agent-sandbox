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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestObserveCreateLatency(t *testing.T) {
	observeCreateLatency(time.Now().Add(-10*time.Millisecond), nil)
	count := testutil.CollectAndCount(SandboxClientCreateLatencyMS, "sandbox_client_create_latency_ms")
	require.GreaterOrEqual(t, count, 1)
}

func TestObserveDiscoveryLatency(t *testing.T) {
	observeDiscoveryLatency(time.Now().Add(-5*time.Millisecond), "cache", nil)
	count := testutil.CollectAndCount(SandboxClientDiscoveryLatencyMS, "sandbox_client_discovery_latency_ms")
	require.GreaterOrEqual(t, count, 1)
}
