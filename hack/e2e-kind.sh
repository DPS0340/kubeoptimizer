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

# --namespace: default keeps its findings; kube-system must not leak in,
# and cluster-scoped findings (the unbound PV) must disappear.
NSOUT=$(/tmp/kubeoptimizer scan --namespace default --output json)
echo "$NSOUT" | jq -e '.findings[] | select(.check == "idle-loadbalancer" and .target == "svc/default/e2e-idle-lb")' >/dev/null
echo "$NSOUT" | jq -e '[.findings[] | select(.target | test("kube-system"))] | length == 0' >/dev/null
echo "$NSOUT" | jq -e '[.findings[] | select(.target == "pv/e2e-unbound-pv")] | length == 0' >/dev/null
echo "$NSOUT" | jq -e '.notes[] | select(test("limited to namespace"))' >/dev/null

# --fail-over exit-code contract: 2 when over threshold, 0 when under.
set +e
/tmp/kubeoptimizer scan --fail-over 0.01 --output json >/dev/null
RC=$?
set -e
[ "$RC" -eq 2 ] || { echo "fail-over: expected exit 2, got $RC"; exit 1; }
/tmp/kubeoptimizer scan --fail-over 999999 --output json >/dev/null

echo "E2E smoke: all expected findings present ✓"
