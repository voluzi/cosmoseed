#!/usr/bin/env bash
set -euo pipefail

helm_bin=${1:-helm}
chart_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
long_name=$(printf 'a%.0s' {1..63})

render=$(
  "$helm_bin" template test "$chart_dir" \
    --set config.chainID=test-chain \
    --set replicaCount=3 \
    --set fullnameOverride="$long_name" \
    --set-string 'config.externalAddresses[0]=seed0.example:443' \
    --set-string 'config.externalAddresses[1]=127.0.0.1:65535' \
    --set-string 'config.externalAddresses[2]= [2001:db8::1]:00001 '
)

printf '%s\n' "$render" | awk '
  /^kind: (Service|StatefulSet)$/ { kind=$2; next }
  kind && /^  name: / {
    if (length($2) > 63) { print "resource name exceeds 63 characters: " $2 > "/dev/stderr"; exit 1 }
    kind=""
  }
'
for ordinal in 0 1 2; do
  if ! printf '%s\n' "$render" | grep -q "statefulset.kubernetes.io/pod-name: .*-${ordinal}$"; then
    echo "missing pod-specific P2P selector for ordinal $ordinal" >&2
    exit 1
  fi
done

assert_service() {
  local service_name=$1 expected_port=$2 expected_target=$3 expected_publish=$4
  printf '%s\n' "$render" | awk -v wanted="$service_name" -v expected_port="$expected_port" -v expected_target="$expected_target" -v expected_publish="$expected_publish" '
    function verify() {
      if (kind != "Service" || name != wanted) return
      found++
      if (("" port) != ("" expected_port) || target != expected_target || publish != expected_publish) {
        printf "Service %s: port=%s targetPort=%s publishNotReadyAddresses=%s\n", wanted, port, target, publish > "/dev/stderr"
        failed=1
        exit 1
      }
    }
    /^---$/ { verify(); kind=""; name=""; port=""; target=""; publish="absent"; next }
    /^kind: / { kind=$2 }
    /^  name: / && name == "" { name=$2 }
    /^      port: / { port=$2 }
    /^      targetPort: / { target=$2 }
    /^  publishNotReadyAddresses: / { publish=$2 }
    END { if (failed) exit 1; verify(); if (found != 1) { printf "expected one Service %s, found %d\n", wanted, found > "/dev/stderr"; exit 1 } }
  '
}

service_base=${long_name:0:54}
assert_service "$service_base" 8080 http absent
assert_service "$service_base-metrics" 9090 metrics true
assert_service "$service_base-headless" 26656 p2p true
assert_service "$service_base-0-p2p" 443 p2p absent
assert_service "$service_base-1-p2p" 65535 p2p absent
assert_service "$service_base-2-p2p" 1 p2p absent

for address in 'seed.example' 'seed.example:' 'seed.example:0' 'seed.example:65536' 'seed.example:http' '2001:db8::1:443' '[2001:db8::1]' '[2001:db8::1]:http' ':443' '   '; do
  if invalid_render=$("$helm_bin" template test "$chart_dir" --set config.chainID=test-chain --set-string "config.externalAddresses[0]=$address" 2>&1); then
    echo "invalid external address accepted: $address" >&2
    exit 1
  fi
  if [[ "$invalid_render" != *config.externalAddresses* ]]; then
    echo "invalid external address error lacks context ($address): $invalid_render" >&2
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
