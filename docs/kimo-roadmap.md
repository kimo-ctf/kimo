# KIMO Roadmap

**Date**: 2026-08-12 (status updated 2026-08-15)

Milestones map onto the task groups in `docs/kimo-impl.md`. Each one has a concrete exit criteria — something you can point `kubectl` or `curl` at — rather than "tasks done."

**Status at a glance:** Phases 0–3, 5, 6 done and verified (unit tests, a real envtest integration suite, and live manual validation against a real kind cluster with the real built image). Phase 4 (Discord bot) was implemented once, then reverted, and is not currently in the tree — still an open decision, see below. Along the way, actually running the system (not just unit-testing it) surfaced and fixed several real bugs the fake-client unit tests couldn't catch: `restartPolicy: OnFailure` crash-looping Deployments, a read-only root filesystem with no writable `/tmp` breaking ordinary images, the Instance Controller never watching its own Pods so phase transitions only advanced on a 15s timer instead of reactively, and `NetworkFence` never actually getting created so per-instance network isolation was silently a no-op.

## Phase 0 — Scaffold ✅
**Tasks:** 0
**Exit criteria:** `go build ./...` succeeds on an empty kubebuilder project (manager binary, no controllers registered yet).

## Phase 1 — Core CRDs & Controllers (the walking skeleton) ✅
**Tasks:** 1–5, 9–11
**Exit criteria:** apply a `ChallengeTemplate` + `ChallengeInstance` by hand with `kubectl`, watch it go `Pending → Creating → Running`, watch it get network-fenced, watch it expire on TTL and get cleaned up. No REST API, no backend integration, no bot — everything driven directly through the K8s API. This is the phase that proves the container lifecycle state machine actually works against real Pod status, not just fake-client unit tests.

## Phase 2 — Scoring Backend Integration Layer ✅
**Tasks:** 6–8
**Exit criteria:** the `Backend` interface + registry exist and are unit-tested; the `generic` (HMAC webhook) adapter is the default; the `ctfd` adapter exists as the worked "second backend" proof that adding a platform doesn't touch controller code. Not wired into the manager yet — that's Phase 3.

## Phase 3 — REST API & PoW ✅
**Tasks:** 12–15
**Exit criteria:** `kimo-manager` runs with the API server embedded, `POST /api/v1/instances` provisions a real `ChallengeInstance`, PoW is enforced when a template requires it, and auth is delegated to whichever `Backend` is configured via `KIMO_BACKEND`. This is the first point where an external scoring platform could actually integrate.

## Phase 4 — Discord Bot ⏸️ reverted, open decision
**Tasks:** 16–18
**Exit criteria:** `/instance create`, `/instance list`, `/monitor start` work against a real KIMO API server; event embeds show up when an instance changes phase.

Was implemented, then reverted in a single commit with no recorded reason; `internal/bot`/`cmd/bot` and the `discordgo` dependency don't exist in the tree. Not redone yet — needs a decision on whether/when to pick it back up.

## Phase 5 — Packaging & Deployment ✅
**Tasks:** 19–20
**Exit criteria:** `helm install kimo ./helm/kimo` stands up the operator + optional bot on a real (or kind) cluster with a one-line backend switch (`--set integration.backend=ctfd`).

Verified for real: built the manager image from the repo `Dockerfile` (which didn't actually build until two real bugs were fixed — a Go import-path gotcha and a `.dockerignore` allowlist pattern that silently doesn't work under Podman/buildah), loaded it into a real kind cluster, `helm install`'d the chart (which needed a leader-election RBAC fix to actually start), and watched a live `ChallengeInstance` reach `Running`.

## Phase 6 — Testing & CI ✅ (except the kind-based e2e run itself)
**Tasks:** 21–24
**Exit criteria:** envtest integration suite and a kind-based e2e suite both green in CI on every PR; sample manifests double as fixtures and as onboarding docs for challenge authors.

`test/integration/` (envtest, real controllers + real watches, not the fake client) is written and passes locally — this is what caught the missing Pod-watch bug. `test/e2e/` has a real challenge-lifecycle scenario now (Deployment/Service/NetworkFence/NetworkPolicy/Running, then cleanup), compiles and vets clean, but wasn't run to completion in this sandbox: it depends on `kind load docker-image`, which real CI's genuine Docker handles fine but which this environment's sibling-podman kind setup can't do (confirmed; `kind load image-archive` works here instead, but patching the shared `test/utils/utils.go` to route through that would be bending real test infra around a sandbox-only limitation). `make lint` was actually failing (55 issues) until fixed — CI's Lint job would have been red. Sample manifests reflect what's actually been verified working, not just what the schema allows.

**Local kind, verified working:** `hack/kind.sh` runs kind/kubectl as sibling containers against the host's rootless podman over its API socket (`hack/kind-runner.Dockerfile`), confirmed end-to-end — cluster comes up, `kubectl get nodes` reports Ready, teardown is clean. This replaces the docker-in-docker approach in `.devcontainer/e2e/`, which was tested and hangs indefinitely in this environment: three layers of nesting (host → podman → DinD dockerd → kind's own container) causes the `kindest/node` image pull to wedge with zero progress. The sibling/socket approach removes a nesting layer and works. One gotcha: pulling `kindest/node` *through* the socket from inside the runner container is itself unreliable (same wedging behavior) — pre-pull it directly on the host first (`podman pull docker.io/kindest/node:v1.36.1`) so kind's own image-check just finds it cached. `.devcontainer/e2e/` is left as-is for real CI/Linux-Docker hosts where DinD is normal and reliable; `hack/kind.sh` is the one to reach for locally in a rootless-Podman-only environment like this one.

## Phase 7 (stretch, not yet committed) — Category-driven hardening
Not in the task list above — only pursue this once Phase 1 is stable and there's an actual pwn/reversing challenge category to harden for.

- **Near-term, low-cost:** extend `ContainerSpec` with an isolation profile (`runtimeClassName` + seccomp/capabilities preset), selectable per `ChallengeTemplate.spec.category` with a template-level override. Covers "pwn gets gVisor/Kata + tight seccomp, web stays on runc" using stock Kubernetes fields — no node agent to operate.
- **Only if that's insufficient:** an NRI plugin (to wire a container's cgroup into policy at creation time, keyed off the category label) backed by an eBPF/LSM enforcer (à la Tetragon) for argument-level or dynamically-updatable syscall policy. This is real node-level infrastructure — worth it only if RuntimeClass + static seccomp genuinely can't express the policy needed.

---

**Sequencing note:** Phases 1 and 2 can run in parallel once Phase 0 is done — the Instance/Lifecycle controllers only need the `Backend` *interface* (already defined in Phase 2's first task) to compile against, not a finished adapter. Phase 3 is the first hard dependency point where they merge.
