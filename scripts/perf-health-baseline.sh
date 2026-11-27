#!/usr/bin/env bash
set -Eeuo pipefail

BASE_URL="${BASE_URL:?Set BASE_URL to the API origin}"
BASE_URL="${BASE_URL%/}"
REQUESTS="${REQUESTS:-100}"
CONCURRENCY="${CONCURRENCY:-10}"
CURL="${CURL:-curl}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

for value in "$REQUESTS" "$CONCURRENCY"; do
  if [[ ! "$value" =~ ^[0-9]+$ ]]; then
    echo "REQUESTS and CONCURRENCY must be positive integers" >&2
    exit 2
  fi
done
if (( REQUESTS < 1 || REQUESTS > 5000 || CONCURRENCY < 1 || CONCURRENCY > 50 )); then
  echo "Bounds: REQUESTS=1..5000 and CONCURRENCY=1..50" >&2
  exit 2
fi
command -v "$CURL" >/dev/null
command -v xargs >/dev/null
export BASE_URL CURL

echo "==> Running $REQUESTS read-only /healthz requests at concurrency $CONCURRENCY"
seq 1 "$REQUESTS" | xargs -n 1 -P "$CONCURRENCY" sh -c \
  '"$CURL" --silent --show-error --output /dev/null --connect-timeout 3 --max-time 10 --write-out "%{http_code} %{time_total}\n" "$BASE_URL/healthz"' \
  >"$TMP_DIR/results"

if ! awk '$1 != "200" { failed++ } END { if (failed > 0) exit 1 }' "$TMP_DIR/results"; then
  echo "At least one health request did not return HTTP 200" >&2
  awk '$1 != "200" { print "HTTP status:", $1 }' "$TMP_DIR/results" >&2
  exit 1
fi

awk '{ print $2 }' "$TMP_DIR/results" | sort -n >"$TMP_DIR/latencies"
count="$(wc -l <"$TMP_DIR/latencies" | tr -d ' ')"
p50_index=$(( (count + 1) / 2 ))
p95_index=$(( (count * 95 + 99) / 100 ))
p50="$(sed -n "${p50_index}p" "$TMP_DIR/latencies")"
p95="$(sed -n "${p95_index}p" "$TMP_DIR/latencies")"
awk -v p50="$p50" -v p95="$p95" -v count="$count" 'BEGIN {
  printf "Completed: %d; p50: %.1f ms; p95: %.1f ms\n", count, p50 * 1000, p95 * 1000
}'
