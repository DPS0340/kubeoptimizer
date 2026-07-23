# kubeoptimizer

**Read-only Kubernetes cost waste scanner.** Point it at any cluster
with a kubeconfig and get an estimated monthly waste report in ~30
seconds. No agent, no telemetry, no mutations — `get`/`list` only.

```
$ kubeoptimizer scan

kubeoptimizer scan — https://my-cluster

EST./MO    CONF      CHECK                     TARGET              REASON
$2233.80   estimate  idle-gpu                  node/gpu-node-1     node has 1 GPU(s), zero requested by any pod
$140.16    estimate  underutilized-nodes       node/worker-7       requests at 12% CPU / 31% memory of allocatable
$10.00     certain   unused-pv                 pv/pvc-8f3a...      PersistentVolume is Released — not bound to any claim
...
TOTAL      $2402.51/mo estimated waste (9 findings)
```

## Install

```
go install github.com/DPS0340/kubeoptimizer@latest
```

## What it finds

| Check | Needs | What it catches |
|---|---|---|
| `overprovisioned-requests` | metrics-server | requests far above live usage (rough right-sizing) |
| `underutilized-nodes` | — | nodes under 50% requested — consolidation candidates |
| `idle-gpu` | — | GPU nodes with zero GPU requests |
| `unused-pv` | — | Released/unbound PVs, PVCs no pod mounts |
| `idle-loadbalancer` | — | LoadBalancer services with no ready endpoints |
| `zombie-workloads` | — | long-term CrashLoop (reserved requests), stale finished pods/jobs |
| `no-requests` | — | containers without resource requests (unpredictable cost) |

Data sources are auto-detected. No metrics-server? API-only checks
still run and the report says exactly what was skipped.

## Pricing model

Node costs come from an embedded on-demand pricing table keyed by the
`node.kubernetes.io/instance-type` label, falling back to per-resource
rates. On-prem? Override with `--cpu-rate` / `--mem-rate`. Every dollar
figure carries its derivation and a confidence level (`certain` /
`estimate`) — no inflated numbers.

## Security

- **Read-only by construction:** no mutating API verbs exist in the codebase.
- Minimal RBAC: [`deploy/rbac.yaml`](deploy/rbac.yaml).
- Zero network calls besides the Kubernetes API. No telemetry, ever.

## Roadmap

Free (this repo): everything above. Planned paid tier: Prometheus-based
p95/p99 precision right-sizing, HTML executive reports, trend tracking,
CI cost-regression mode, multi-cluster aggregation. Full details and
status: [ROADMAP.md](ROADMAP.md).

## License

Apache-2.0
