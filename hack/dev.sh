#!/usr/bin/env bash
# Runs a command inside the kimo-dev container (Go 1.26 + kubebuilder v4.15),
# with the repo mounted read-write and Go's module/build caches persisted on
# the host so repeated runs don't re-download dependencies. Nothing this
# script uses gets installed outside of containers.
#
# Usage:
#   hack/dev.sh go build ./...
#   hack/dev.sh go test ./...
#   hack/dev.sh kubebuilder create api --group kimo --version v1alpha1 --kind ChallengeTemplate --resource --controller
#   hack/dev.sh make build

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="kimo-dev:latest"
CACHE_DIR="${HOME}/.cache/kimo-dev"

mkdir -p "${CACHE_DIR}/gomod" "${CACHE_DIR}/gocache"

PODMAN_TTY_FLAGS="-i"
if [ -t 0 ] && [ -t 1 ]; then
  PODMAN_TTY_FLAGS="-it"
fi

exec podman run --rm ${PODMAN_TTY_FLAGS} \
  -v "${REPO_ROOT}:/workspace:Z" \
  -v "${CACHE_DIR}/gomod:/go/pkg/mod:Z" \
  -v "${CACHE_DIR}/gocache:/tmp/gocache:Z" \
  -w /workspace \
  -e HOME=/tmp \
  -e GOCACHE=/tmp/gocache \
  "${IMAGE}" \
  "$@"
