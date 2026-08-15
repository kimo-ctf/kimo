# Dev-only image used by hack/kind.sh: kind + kubectl + a remote podman
# client, nothing else. Used to drive kind against the HOST's rootless
# podman over its API socket (sibling containers), not nested
# docker-in-docker — DinD-in-rootless-Podman was tested and hangs on the
# node image pull in this environment; the socket/sibling approach is the
# one that actually works here.
#
# IMPORTANT: podman-remote's version MUST match the host podman server's
# version (check with `podman version` on the host). A "latest" client
# against an older server causes kind's podman provider to silently
# misbehave — e.g. `kind load docker-image` reports an image "not present
# locally" even though `podman image exists`/`podman images` on the exact
# same client see it fine. Bumping this requires re-checking the host
# version first.
#
# Build once with:
#   podman build -t kind-runner:latest -f hack/kind-runner.Dockerfile hack/

FROM docker.io/library/golang:1.26

ARG PODMAN_VERSION=5.8.4

RUN curl -Lo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64" \
    && chmod +x /usr/local/bin/kind \
    && KUBECTL_VERSION=$(curl -Ls https://dl.k8s.io/release/stable.txt) \
    && curl -Lo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
    && chmod +x /usr/local/bin/kubectl \
    && curl -L "https://github.com/containers/podman/releases/download/v${PODMAN_VERSION}/podman-remote-static-linux_amd64.tar.gz" \
       | tar -xz -C /usr/local/bin --strip-components=1 \
    && mv /usr/local/bin/podman-remote-static-linux_amd64 /usr/local/bin/podman

WORKDIR /workspace
