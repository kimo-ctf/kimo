#!/usr/bin/env bash
# Runs kind/kubectl against the HOST's rootless podman, via its API socket,
# using sibling containers rather than nested docker-in-docker. Nothing
# gets installed on the host beyond the podman.socket user service (which
# ships with podman itself) and the kind-runner container image.
#
# Why not docker-in-docker: tested in this environment and it hung
# indefinitely pulling the kindest/node image inside triple-nested
# containers (host -> podman -> DinD dockerd -> kind's own container).
# The socket/sibling approach avoids that extra nesting layer entirely.
#
# IMPORTANT: the kindest/node image is large (~1GB) and podman-remote's
# pull-through-the-socket path is unreliable in this setup — it can hang
# with zero progress. Pre-pull it directly on the host first so kind's
# "Ensuring node image" step just finds it cached instead of trying to
# pull it itself:
#   podman pull docker.io/kindest/node:v1.36.1
#
# Usage:
#   hack/kind.sh kind create cluster --name kimo-test
#   hack/kind.sh kind get kubeconfig --name kimo-test > /tmp/kimo-test.kubeconfig
#   hack/kind.sh kubectl --kubeconfig /tmp/kimo-test.kubeconfig get nodes
#   hack/kind.sh kind delete cluster --name kimo-test

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="kind-runner:latest"
SOCK="/run/user/$(id -u)/podman/podman.sock"

if [ ! -S "${SOCK}" ]; then
  echo "Starting rootless podman API socket (user-scoped, systemd --user)..." >&2
  systemctl --user start podman.socket
fi

exec podman run --rm -i --network host \
  -v "${SOCK}:/run/podman/podman.sock:Z" \
  -v "${REPO_ROOT}:/workspace:Z" \
  -v /tmp:/tmp:Z \
  -w /workspace \
  -e CONTAINER_HOST=unix:///run/podman/podman.sock \
  -e KIND_EXPERIMENTAL_PROVIDER=podman \
  "${IMAGE}" \
  "$@"
