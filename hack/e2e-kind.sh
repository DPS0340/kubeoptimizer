#!/usr/bin/env bash
# E2E smoke: plant known waste in a kind cluster, expect the scanner
# to find it. Requires: kind, docker, kubectl, jq.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER=kubeoptimizer-e2e
kind create cluster --name "$CLUSTER" --wait 120s
trap 'kind delete cluster --name "$CLUSTER"' EXIT

kubectl apply -f hack/fixtures.yaml
kubectl wait --for=condition=Ready pod/e2e-no-requests --timeout=60s

go build -o /tmp/kubeoptimizer .
OUT=$(/tmp/kubeoptimizer scan --output json)

echo "$OUT" | jq -e '.findings[] | select(.check == "unused-pv" and .target == "pv/e2e-unbound-pv")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "idle-loadbalancer" and .target == "svc/default/e2e-idle-lb")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "no-requests")' >/dev/null
echo "E2E smoke: all expected findings present ✓"
