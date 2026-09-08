#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
record_count="${VERITY_LOAD_RECORDS:-100000}"
api_port="${VERITY_LOAD_PORT:-18100}"
load_root="$(mktemp -d "${TMPDIR:-/tmp}/verity-load.XXXXXX")"
api_pid=""
monitor_pid=""

cleanup() {
  if [[ -n "$monitor_pid" ]]; then
    kill "$monitor_pid" 2>/dev/null || true
    wait "$monitor_pid" 2>/dev/null || true
  fi
  if [[ -n "$api_pid" ]]; then
    kill "$api_pid" 2>/dev/null || true
    wait "$api_pid" 2>/dev/null || true
  fi
  case "$load_root" in
    "${TMPDIR:-/tmp}"/verity-load.*) rm -rf "$load_root" ;;
  esac
}
trap cleanup EXIT

mkdir -p "$load_root/raw" "$load_root/artifacts" "$repo_root/artifacts/load"
source_path="$load_root/noisy-workflow-events-${record_count}.jsonl"
api_binary="$load_root/verity-api"
cli_binary="$load_root/verity"

cd "$repo_root/apps/api"
go build -o "$api_binary" ./cmd/verity-api
go build -o "$cli_binary" ./cmd/verity
go run ./cmd/verity-generate \
  --records "$record_count" \
  --format jsonl \
  --seed 20260906 \
  --noise-profile mixed \
  --output "$source_path"

VERITY_REPO_ROOT="$repo_root" \
VERITY_ARTIFACT_ROOT="$load_root/artifacts" \
VERITY_RAW_DIR="$load_root/raw" \
VERITY_API_ADDR="127.0.0.1:${api_port}" \
VERITY_ALLOWED_ORIGINS="http://127.0.0.1:3000" \
"$api_binary" >"$load_root/api.log" 2>&1 &
api_pid=$!

for _ in $(seq 1 300); do
  if curl -fsS "http://127.0.0.1:${api_port}/ready" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -fsS "http://127.0.0.1:${api_port}/ready" >/dev/null

(
  while kill -0 "$api_pid" 2>/dev/null; do
    ps -o rss= -p "$api_pid" 2>/dev/null | tr -d ' ' >>"$load_root/rss-kb.log" || true
    sleep 0.2
  done
) &
monitor_pid=$!

started_at="$(date +%s)"
result_json="$load_root/result.json"
"$cli_binary" import "$source_path" \
  --server "http://127.0.0.1:${api_port}" \
  --wait \
  --output "$load_root/ready.parquet" \
  --json >"$result_json"
finished_at="$(date +%s)"

workspace_json="$load_root/workspace.json"
curl -fsS "http://127.0.0.1:${api_port}/api/v1/workspace" >"$workspace_json"
actual_count="$(jq -r '.stages[] | select(.id == "raw") | .count' "$workspace_json")"
if [[ "$actual_count" != "$record_count" ]]; then
  echo "raw count mismatch: got $actual_count, want $record_count" >&2
  exit 1
fi
jq -e '.job.state == "succeeded" and .download.bytes > 0 and .download.verified == true' "$result_json" >/dev/null

inspection_json='{}'
for stage in raw normalize privacy quality signals review curated; do
  elapsed="$(curl -fsS -o "$load_root/inspection-${stage}.json" -w '%{time_total}' "http://127.0.0.1:${api_port}/api/v1/stages/${stage}/comparison?limit=10")"
  jq -e '.samples | length <= 10' "$load_root/inspection-${stage}.json" >/dev/null
  inspection_json="$(jq --arg stage "$stage" --argjson elapsed "$elapsed" '. + {($stage): $elapsed}' <<<"$inspection_json")"
done
jq -e 'all(.[]; . < 2)' <<<"$inspection_json" >/dev/null

peak_rss_kb="$(sort -nr "$load_root/rss-kb.log" | head -1)"
source_sha256="$(shasum -a 256 "$source_path" | awk '{print $1}')"
output_sha256="$(jq -r '.download.sha256' "$result_json")"
report_path="$repo_root/artifacts/load/e2e-${record_count}.json"
jq -n \
  --argjson records "$record_count" \
  --argjson seconds "$((finished_at - started_at))" \
  --argjson peak_rss_kb "${peak_rss_kb:-0}" \
  --arg source_sha256 "$source_sha256" \
  --arg output_sha256 "$output_sha256" \
  --argjson inspection_seconds "$inspection_json" \
  '{records: $records, wall_seconds: $seconds, peak_api_rss_kb: $peak_rss_kb, source_sha256: $source_sha256, output_sha256: $output_sha256, inspection_seconds: $inspection_seconds, state: "succeeded"}' >"$report_path"

cat "$report_path"
