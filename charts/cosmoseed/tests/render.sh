#!/usr/bin/env bash
set -euo pipefail

helm_bin=${1:-helm}
chart_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
long_name=$(printf 'a%.0s' {1..63})

render=$(
  "$helm_bin" template test "$chart_dir" \
    --set config.chainID=test-chain \
    --set replicaCount=2 \
    --set fullnameOverride="$long_name" \
    --set 'config.externalAddresses[0]=seed0.example:26656' \
    --set 'config.externalAddresses[1]=seed1.example:26656'
)

printf '%s\n' "$render" | awk '
  /^kind: (Service|StatefulSet)$/ { kind=$2; next }
  kind && /^  name: / {
    if (length($2) > 63) { print "resource name exceeds 63 characters: " $2 > "/dev/stderr"; exit 1 }
    kind=""
  }
'
for ordinal in 0 1; do
  if ! printf '%s\n' "$render" | grep -q "statefulset.kubernetes.io/pod-name: .*-${ordinal}$"; then
    echo "missing pod-specific P2P selector for ordinal $ordinal" >&2
    exit 1
  fi
done

if "$helm_bin" template test "$chart_dir" --set config.chainID=test-chain --set existingConfigMap=external >/dev/null 2>&1; then
  echo 'existingConfigMap without checksum was accepted' >&2
  exit 1
fi
"$helm_bin" template test "$chart_dir" --set existingConfigMap=external --set existingConfigMapChecksum=revision-1 >/dev/null

labels=$(
  "$helm_bin" template test "$chart_dir" \
    --set config.chainID=test-chain \
    --set metrics.serviceMonitor.enabled=true \
    --set-string 'commonLabels.app\.kubernetes\.io/name=evil' \
    --set-string 'podLabels.app\.kubernetes\.io/name=evil' \
    --set-string 'metrics.serviceMonitor.labels.app\.kubernetes\.io/name=evil' \
    --set-string 'commonLabels.cosmoseed\.voluzi\.com/service-role=evil'
)
if printf '%s\n' "$labels" | grep -q 'app.kubernetes.io/name: evil'; then
  echo 'user labels replaced a built-in label' >&2
  exit 1
fi
printf '%s\n' "$labels" | awk '
  function indent(line, trimmed) { trimmed=line; sub(/[^ ].*$/, "", trimmed); return length(trimmed) }
  /^[ ]*labels:[ ]*$/ { inside=1; level=indent($0); delete seen; next }
  inside {
    current=indent($0)
    if (current <= level) { inside=0; next }
    if (current == level + 2 && $0 ~ /:[ ]*/) {
      key=$0
      sub(/^[ ]*/, "", key)
      sub(/:.*/, "", key)
      if (seen[key]++) { print "duplicate label: " key > "/dev/stderr"; exit 1 }
    }
  }
'
