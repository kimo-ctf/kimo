# Dev-only image used by hack/dev.sh: Go + kubebuilder CLI, nothing else.
# Not related to the production Dockerfile at the repo root, which builds
# the manager/bot binaries themselves.
#
# Build once with:
#   podman build -t kimo-dev:latest -f hack/kimo-dev.Dockerfile hack/

FROM docker.io/library/golang:1.26

RUN curl -L -o /usr/local/bin/kubebuilder \
      "https://github.com/kubernetes-sigs/kubebuilder/releases/latest/download/kubebuilder_linux_amd64" \
    && chmod +x /usr/local/bin/kubebuilder

WORKDIR /workspace
