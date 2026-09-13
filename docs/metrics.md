# Controller Metrics Reference

The `agent-sandbox-controller` exposes Prometheus metrics on its metrics Service
port (`metrics`, default `:8080`, path `/metrics`). This page catalogs the
**domain** metrics owned by this project. Controller-runtime also exposes
standard reconcile / workqueue series (`controller_runtime_*`, `workqueue_*`);
those are not duplicated here.

## Scraping

- Helm: enable `.Values.metrics.serviceMonitor.enabled` to install a
  `ServiceMonitor`.
- Plain manifests: apply the example
  [`k8s/servicemonitor.yaml`](../k8s/servicemonitor.yaml) (requires
  [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)).

## Label sentinel

Across controller domain metrics, a missing sandbox template name is labeled
`sandbox_template="__unknown__"` (constant `UnknownTemplateSentinel`). Older
inventory scrapes that used `unknown` must update PromQL joins.

## Latency histograms

| Metric | Labels | Meaning |
| --- | --- | --- |
| `agent_sandbox_claim_startup_latency_ms` | `launch_type`, `sandbox_template` | Webhook first-seen annotation → Claim Ready |
| `agent_sandbox_claim_controller_startup_latency_ms` | `launch_type`, `sandbox_template` | Controller first-seen → Claim Ready |
| `agent_sandbox_client_claim_startup_latency_ms` | `launch_type`, `sandbox_template` | Client first-requested annotation → Claim Ready |
| `agent_sandbox_creation_latency_ms` | `namespace`, `launch_type`, `sandbox_template` | Sandbox create → Sandbox Ready |

`launch_type` is `warm`, `cold`, or `unknown`.

## Counters

| Metric | Labels | Meaning |
| --- | --- | --- |
| `agent_sandbox_claim_creation_total` | `namespace`, `sandbox_template`, `launch_type`, `warmpool_name`, `pod_condition`, `created_by` | Sandboxes created or adopted for a claim |
| `agent_sandbox_claim_reconcile_errors_total` | `reason` | Durable Claim Ready=False transitions (allow-listed reasons) |
| `agent_sandbox_reconcile_errors_total` | `reason` | Sandbox reconcile failures (allow-listed reasons) |

Claim error `reason` allow-list includes `TemplateNotFound`, `WarmPoolNotFound`,
`InvalidMetadata`, `ClaimExpired`, `SandboxMissing`, `SandboxExpired`,
`ReconcilerError`, and related durable failures. Expected contention
(`AdoptionConflict`, `SandboxCreatePending`) and in-progress waits
(`SandboxNotReady`) are not counted.

Sandbox error `reason` allow-list: `MultiplePods`, `PodCreateFailed`,
`ServiceCreateFailed`, `PVCCreateFailed`, `StatusUpdateFailed`,
`ReconcilerError`.

## Inventory gauges (custom collectors)

| Metric | Labels | Meaning |
| --- | --- | --- |
| `agent_sandboxes` | `namespace`, `ready_condition`, `expired`, `launch_type`, `sandbox_template`, `owned_by`, `created_by` | Point-in-time Sandbox counts |
| `agent_sandbox_claims` | `namespace`, `ready_condition`, `reason` | Point-in-time Claim counts |
| `agent_sandbox_warmpool_desired_replicas` | `namespace`, `warmpool`, `sandbox_template` | `spec.replicas` |
| `agent_sandbox_warmpool_replicas` | `namespace`, `warmpool`, `sandbox_template` | `status.replicas` |
| `agent_sandbox_warmpool_ready_replicas` | `namespace`, `warmpool`, `sandbox_template` | `status.readyReplicas` |
| `agent_sandbox_warmpool_deficit` | `namespace`, `warmpool`, `sandbox_template` | `max(desired - ready, 0)` |

## Build info

| Metric | Labels | Meaning |
| --- | --- | --- |
| `agent_sandbox_build_info` | git/go/platform const labels | Constant `1` for build metadata |

## WarmPool status conditions

`SandboxWarmPool.status.conditions` now includes:

- `Available` — enough Ready replicas for `spec.replicas`
- `Progressing` — pool is making progress (False when held for unschedulable
  members past the readiness grace period)

These complement the existing `WarmPoolNotProgressing` / `WarmPoolProgressing`
Events and the warm-pool Prometheus gauges above.

## Client SDK metrics

- Python (`k8s_agent_sandbox.metrics`): discovery / suspend / resume / restore
  latency histograms.
- Go (`sigs.k8s.io/agent-sandbox/clients/go/sandbox`):
  `sandbox_client_create_latency_ms{status}`,
  `sandbox_client_discovery_latency_ms{mode,status}`.

## Example PromQL

```promql
# Claim Ready p99 (controller-observed)
histogram_quantile(
  0.99,
  sum by (le, launch_type) (
    rate(agent_sandbox_claim_controller_startup_latency_ms_bucket[5m])
  )
)

# Warm pool deficit
sum by (namespace, warmpool) (agent_sandbox_warmpool_deficit)

# Durable claim failures
sum by (reason) (rate(agent_sandbox_claim_reconcile_errors_total[5m]))
```
