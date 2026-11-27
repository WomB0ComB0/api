#!/usr/bin/env bash
#
# Copyright 2026 Mike Odnis
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# @file Builds the real gateway image from services/gateway/Dockerfile, runs it, waits for
# /gateway/health and runs gateway.test.ts against it. CI and local runs use this same script, so
# a green local run means the same thing as a green CI run.
#
# Every upstream points at port 9 on the container's own loopback, where nothing listens, so no
# request can reach a real service; the test only covers routes the gateway answers itself.
#
# Usage (from anywhere):
#   services/gateway/tests/run.sh                            # docker
#   CONTAINER_ENGINE=podman services/gateway/tests/run.sh    # podman
# Optional: GATEWAY_TEST_PORT (host port, default 8091), GATEWAY_TEST_IMAGE (tag to build).

set -euo pipefail

engine="${CONTAINER_ENGINE:-docker}"
host_port="${GATEWAY_TEST_PORT:-8091}"
image="${GATEWAY_TEST_IMAGE:-api-gateway-test:local}"
name="api-gateway-test-$$"
gateway_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stub_upstream="127.0.0.1:9"
gateway_url="http://127.0.0.1:${host_port}"

cleanup() {
  local status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- gateway container logs ---"
    "$engine" logs "$name" 2>&1 || true
  fi
  "$engine" rm -f "$name" >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

"$engine" build --tag "$image" --file "$gateway_dir/Dockerfile" "$gateway_dir"

"$engine" run --detach --name "$name" \
  --publish "127.0.0.1:${host_port}:8080" \
  --env PORT=8080 \
  --env CDN_WORKER_UPSTREAM="$stub_upstream" \
  --env CORE_UPSTREAM="$stub_upstream" \
  --env MEDIA_UPSTREAM="$stub_upstream" \
  --env HEALTH_UPSTREAM="$stub_upstream" \
  --env HASURA_UPSTREAM="$stub_upstream" \
  --env GATEWAY_PROXY_SECRET=gateway-test-placeholder \
  "$image" >/dev/null

ready=0
for _ in $(seq 1 30); do
  if curl --silent --fail --max-time 2 "${gateway_url}/gateway/health" >/dev/null; then
    ready=1
    break
  fi
  if [ "$("$engine" inspect --format '{{.State.Running}}' "$name")" != "true" ]; then
    echo "::error::gateway container exited before it became healthy"
    exit 1
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "::error::gateway did not answer ${gateway_url}/gateway/health within 30s"
  exit 1
fi

cd "$gateway_dir"
GATEWAY_URL="$gateway_url" bun test ./tests/gateway.test.ts
