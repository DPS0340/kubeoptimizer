#!/usr/bin/env bash
# E2E smoke: plant known waste in a kind cluster, expect the scanner
# to find it. Requires: kind, docker, kubectl, jq.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER=kubeoptimizer-e2e

# Isolate from the user's real kubeconfig: kind create/delete mutate the
# active kubeconfig (and `kind delete` leaves current-context unset), so
# everything below runs against a throwaway config file instead.
KUBECONFIG="$(mktemp -t kubeoptimizer-e2e-kubeconfig.XXXXXX)"
export KUBECONFIG
trap 'rm -f "$KUBECONFIG"' EXIT

kind create cluster --name "$CLUSTER" --wait 120s
trap 'kind delete cluster --name "$CLUSTER"; rm -f "$KUBECONFIG"' EXIT

kubectl apply -f hack/fixtures.yaml
kubectl wait --for=condition=Ready pod/e2e-no-requests --timeout=60s

go build -o /tmp/kubeoptimizer .
OUT=$(/tmp/kubeoptimizer scan --output json)

echo "$OUT" | jq -e '.findings[] | select(.check == "unused-pv" and .target == "pv/e2e-unbound-pv")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "idle-loadbalancer" and .target == "svc/default/e2e-idle-lb")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "no-requests")' >/dev/null
echo "E2E smoke: all expected findings present ✓"
