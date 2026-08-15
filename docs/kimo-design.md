# KIMO Design Document

**Date**: 2026-08-12
**Status**: Approved (revision 2)

**Revision 2 changes:** dropped VM/KubeVirt runtime support — KIMO is container-only now. Replaced the flat "generic webhook" integration with a pluggable `ScoringBackend` adapter architecture, and deepened the container instance lifecycle model (explicit phases driven by real Pod health, not just TTL bookkeeping).

## Overview

KIMO (Kubernetes Instance Manager Operator) is a Go-based Kubernetes operator that deploys and manages CTF challenge instances as containers. It provides on-demand provisioning, a real container lifecycle state machine driven by Pod health, automatic TTL-based cleanup, built-in network isolation, PoW-based abuse prevention, a pluggable backend layer for integrating with any scoring platform, and a Discord bot for organizer management.

## Requirements

- **Runtime**: Containers only (Pods/Deployments)
- **Isolation**: Configurable per challenge (shared / per-team / per-player)
- **Language**: Go with kubebuilder/controller-runtime
- **Integration**: Pluggable scoring-platform backends — a built-in generic REST+webhook backend that works out of the box, plus first-class adapters for common platforms and a documented interface for adding new ones
- **Scale**: 500+ concurrent instances
- **Networking**: Built-in NetworkPolicy management
- **Lifecycle**: On-demand provisioning, a real health-driven state machine, and configurable TTL auto-cleanup
- **PoW**: Proof of Work to prevent instance provisioning abuse
- **Discord**: Bot for challenge management and monitoring

## Architecture

Single operator binary with multiple controllers, plus a separate Discord bot binary. Both deployed via Helm in the `kimo-system` namespace.

```
                    +-----------------+
                    |  Scoring Platform|
                    | (CTFd / rCTF /   |
                    |  custom / ...)   |
                    +--------+--------+
                             |
                    REST API (Bearer auth)
                             |
                    +--------v--------+
                    |   KIMO API      |
                    |   Server        |
                    +--------+--------+
                             |
                    Creates ChallengeInstance CRs
                             |
         +-------------------+-------------------+
         |                   |                   |
+--------v------+  +---------v------+  +---------v------+
| Instance      |  | Network        |  | Lifecycle      |
| Controller    |  | Controller     |  | Controller     |
| (Pod health   |  |                |  | (TTL + expiry) |
|  state machine)|  |                |  |                |
+-------+-------+  +-------+--------+  +-------+--------+
        |                   |                   |
      Pods            NetPol / Ingress    lifecycle events
        |                                        |
        +----------------------------------------+
                             |
                    +--------v--------+
                    | Backend Registry|
                    +--------+--------+
                             |
              +--------------+--------------+
              |              |              |
        +-----v----+   +-----v----+   +-----v----+
        | generic  |   |  ctfd    |   |  rctf    |   ...custom
        | (webhook)|   |          |   |          |
        +----------+   +----------+   +----------+
```

Instance and Lifecycle controllers emit lifecycle events (phase transitions) into the **Backend Registry**, which fans them out to whichever `ScoringBackend` adapter is configured for the deployment. This is the single integration seam — every scoring platform integration goes through it, including the built-in generic REST/webhook mode.

## Custom Resource Definitions

API group: `kimo.io/v1alpha1`

### ChallengeTemplate

Blueprint for a challenge, created by challenge authors. Containers only — there is no runtime switch.

```yaml
apiVersion: kimo.io/v1alpha1
kind: ChallengeTemplate
metadata:
  name: web-sqli-101
spec:
  category: web
  difficulty: easy
  points: 100
  flagSecretRef:
    name: web-sqli-101-flag
  instanceMode: perTeam       # shared | perTeam | perPlayer
  ttl: 30m
  maxInstances: 200
  pow:
    enabled: true
    difficulty: 20            # leading zero bits
    algorithm: sha256
    ttl: 5m
  container:
    image: registry.ctf.io/challenges/web-sqli:v1
    ports:
      - name: http
        containerPort: 8080
        expose: true
    resources:
      requests: { cpu: 100m, memory: 128Mi }
      limits: { cpu: 500m, memory: 256Mi }
    env:
      - name: FLAG
        valueFrom:
          secretKeyRef: { name: web-sqli-101-flag, key: flag }
    readiness:                 # optional; drives Pending -> Running transition
      type: tcp                # tcp | http | none (defaults to tcp on first exposed port)
      port: 8080
      path: /healthz           # only for type: http
    restartPolicy: Always        # only Always is supported today — instances are
                                  # Deployment-managed and Kubernetes requires Always
                                  # for those; OnFailure/Never are reserved for future
                                  # bare-Pod support and are rejected by the Template
                                  # Controller until then
    unhealthyThreshold: 3       # consecutive failed readiness checks before phase -> Unhealthy
status:
  ready: true
  instanceCount: 42
```

### ChallengeInstance

A running instance of a challenge. The status phase now reflects real Pod health, not just bookkeeping.

```yaml
apiVersion: kimo.io/v1alpha1
kind: ChallengeInstance
metadata:
  name: web-sqli-101-team-42
  labels:
    kimo.io/challenge: web-sqli-101
    kimo.io/team: team-42
spec:
  templateRef: web-sqli-101
  team: team-42
  player: ""
  ttlOverride: 45m
status:
  phase: Running               # Pending | Creating | Running | Unhealthy | Expiring | Expired | Terminating | Failed
  reason: ""                   # short machine-readable reason for the current phase
  endpoint: "https://web-sqli-101-team-42.ctf.example.com"
  startedAt: "2026-08-12T14:00:00Z"
  expiresAt: "2026-08-12T14:45:00Z"
  podName: web-sqli-101-team-42-xyz
```

**Phase state machine:**

```
Pending --> Creating --> Running <--> Unhealthy --> Expiring --> Expired --> Terminating
   |            |            |             |
   +----------- +----------- +-------------+--> Failed (terminal, unrecoverable)
```

- **Pending**: CR accepted, waiting on template readiness or capacity (`maxInstances`).
- **Creating**: Deployment/Service objects created, waiting for the Pod to exist and pass its first readiness check.
- **Running**: Pod passes readiness. Instance Controller watches the owned Pod continuously.
- **Unhealthy**: Pod exists but has failed readiness `unhealthyThreshold` times in a row, or is crash-looping. Instance stays reachable in the CR but the phase signals degraded state to the backend and the scoring platform.
- **Expiring**: within a short grace window of `expiresAt` (default 60s) — gives the backend a chance to notify players before teardown.
- **Expired**: `expiresAt` has passed; Lifecycle Controller has marked it for deletion.
- **Terminating**: owned resources are being deleted.
- **Failed**: unrecoverable error (bad template, invalid TTL, image pull failure past a retry budget). Terminal — requires operator/organizer intervention.

Every transition triggers a `Notify` call into the Backend Registry (see below).

### ChallengeSet

Groups related challenges for bulk operations.

```yaml
apiVersion: kimo.io/v1alpha1
kind: ChallengeSet
spec:
  challenges:
    - web-sqli-101
    - web-xss-201
  schedule:
    startAt: "2026-08-12T10:00:00Z"
    endAt: "2026-08-12T18:00:00Z"
```

### NetworkFence

Per-instance network isolation policy, auto-created by the Network Controller.

```yaml
apiVersion: kimo.io/v1alpha1
kind: NetworkFence
spec:
  instanceRef: web-sqli-101-team-42
  allow:
    - cidr: 10.0.42.0/24
    - port: 8080
  deny:
    - to: kimo-system
```

## Controllers

### Template Controller
- Validates ChallengeTemplate specs (image, resources, flag secret, container spec required, `restartPolicy` must be `Always`/omitted — instances are Deployment-managed and Kubernetes rejects any other value there)
- Sets `status.ready` and tracks `status.instanceCount`
- Enforces `maxInstances` cap

### Instance Controller
- Creates Deployment + Service for the instance's container spec
- Injects per-instance flags
- Watches the owned Pod's status/conditions and drives the phase state machine above (Pending → Creating → Running ↔ Unhealthy → …)
- Calls `Backend.Notify` on every phase transition
- Cleanup via OwnerReferences on delete

### Network Controller
- Creates per-team/player namespaces based on `instanceMode`
- Creates NetworkPolicy per instance:
  - Ingress: only from assigned team or KIMO ingress
  - Egress: blocks K8s API, kimo-system; optionally allows internet
  - Inter-instance: denied by default
- Creates Ingress resources: `{instance-name}.{ctf-domain}`
- For TCP/UDP: NodePort or LoadBalancer Services

### Lifecycle Controller
- Periodic reconciliation (30s interval)
- Moves instances into `Expiring` within the grace window, then `Expired` where `expiresAt < now`
- Handles TTL extensions
- Grace period before deletion; hands off to Instance Controller's `Terminating` cleanup

### Set Controller
- Manages bulk start/stop of challenge groups
- Enforces schedule windows

## Scoring Backend Integrations

This is the primary extension point for connecting KIMO to a scoring platform. Every integration — including the default one — implements the same interface, so adding support for a new platform never touches the controllers.

### The `Backend` interface

```go
package integrations

type EventType string

const (
    EventCreating  EventType = "instance.creating"
    EventRunning   EventType = "instance.running"
    EventUnhealthy EventType = "instance.unhealthy"
    EventExpiring  EventType = "instance.expiring"
    EventExpired   EventType = "instance.expired"
    EventFailed    EventType = "instance.failed"
    EventDeleted   EventType = "instance.deleted"
)

type Event struct {
    Type      EventType
    Instance  string
    Challenge string
    Team      string
    Player    string
    Endpoint  string
    Reason    string
    Timestamp time.Time
}

// Principal identifies the caller of a KIMO API request, as resolved by
// the active backend's own auth scheme (API key, JWT, HMAC, ...).
type Principal struct {
    Subject string
    Team    string
    Scopes  []string
}

// Backend is implemented by every scoring-platform integration.
type Backend interface {
    Name() string
    // Notify is called on every ChallengeInstance phase transition.
    Notify(ctx context.Context, event Event) error
    // Authenticate resolves the caller of an inbound KIMO API request.
    Authenticate(r *http.Request) (Principal, error)
}
```

### Registry

Backends self-register at init time; the manager picks exactly one active backend from config at startup.

```go
package integrations

var registry = map[string]func(cfg json.RawMessage) (Backend, error){}

func Register(name string, factory func(cfg json.RawMessage) (Backend, error)) {
    registry[name] = factory
}

func New(name string, cfg json.RawMessage) (Backend, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown scoring backend %q", name)
    }
    return factory(cfg)
}
```

### Built-in adapters

| Backend | Notify behavior | Authenticate behavior |
|---|---|---|
| `generic` (default) | HMAC-SHA256-signed webhook POSTs to one or more registered URLs (today's design, kept as the zero-config fallback) | Static Bearer API key |
| `ctfd` | Translates events into CTFd's webhook payload shape, posts to CTFd's configured endpoint | Validates CTFd API tokens against CTFd's `/api/v1/users/me` |
| `rctf` | Translates events into rCTF's event format | Validates rCTF JWTs |

`generic` requires zero backend-specific code from an operator — it's the same REST API + HMAC webhook mechanism as before, just expressed as one adapter implementation instead of being baked into the API server.

### Adding a new backend

1. Implement the `Backend` interface in a new file under `internal/integrations/` (typically ~50-100 lines: a `Notify` translator and an `Authenticate` call to the platform's auth endpoint).
2. Register it in an `init()`: `integrations.Register("myplatform", NewMyPlatformBackend)`.
3. Select it via Helm values (`integration.backend: myplatform`) and supply its config.

No dynamic plugin loading (Go plugins aren't portable across platforms/build configs) — new backends are compiled in via a PR to `internal/integrations/`, same as any other Go package. This keeps the extension point simple and testable instead of promising a plugin system KIMO can't reliably support.

### Configuration

```yaml
# Helm values.yaml
integration:
  backend: ctfd          # generic | ctfd | rctf | <custom, once registered>
  config:                # backend-specific, passed through as opaque JSON
    baseUrl: https://ctf.example.com
    apiKeySecretRef: ctfd-api-key
```

Loaded once at manager startup; the resolved `Backend` is injected into the REST API server (for `Authenticate`) and into the Instance/Lifecycle controllers (for `Notify`).

## REST API

Deployed as a Service in `kimo-system`. Auth is delegated to the active backend's `Authenticate`.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/instances` | Provision instance (accepts `powSolution`) |
| `GET` | `/api/v1/instances` | List instances (filter by team, challenge, status) |
| `GET` | `/api/v1/instances/{name}` | Get instance details + endpoint |
| `DELETE` | `/api/v1/instances/{name}` | Destroy instance |
| `PATCH` | `/api/v1/instances/{name}/extend` | Extend TTL |
| `GET` | `/api/v1/templates` | List available templates |
| `GET` | `/api/v1/templates/{name}` | Get template details |
| `GET` | `/api/v1/pow/challenge` | Get PoW puzzle |
| `POST` | `/api/v1/webhooks/configure` | Register a webhook URL (generic backend only) |
| `GET` | `/api/v1/health` | Health check |

## Proof of Work

Configurable per ChallengeTemplate. Enforced by the API server before provisioning.

1. Client requests puzzle: `GET /api/v1/pow/challenge?template=web-sqli-101`
2. Server returns SHA-256 partial preimage challenge with difficulty and TTL
3. Client solves locally, submits nonce with provisioning request
4. Server verifies before creating ChallengeInstance CR

Difficulty tunable per challenge.

## Discord Bot

Separate binary (`cmd/bot/`), deployed alongside the operator. Communicates via KIMO REST API — it never talks to a scoring backend directly, so it's unaffected by which `ScoringBackend` adapter is active.

### Commands

| Command | Description |
|---------|-------------|
| `/challenges list` | List templates with status |
| `/challenges status [name]` | Instance counts, resource usage |
| `/instance create <template> <team>` | Provision instance |
| `/instance destroy <name>` | Tear down instance |
| `/instance extend <name> [duration]` | Extend TTL |
| `/instance list [--team] [--challenge]` | List with filters |
| `/monitor start [channel]` | Stream events to channel |
| `/monitor stop` | Stop streaming |
| `/stats` | Overview dashboard |

### Event Streaming

Registers as a `generic`-backend webhook consumer when that backend is active. Posts formatted embeds to monitoring channels on instance status changes. (When a non-generic backend is active, event streaming comes from that platform's own webhooks if it has them — the bot's `/monitor` commands document this per-backend.)

### RBAC

Discord roles map to API permissions:
- **Admin**: full access
- **Organizer**: view all, manage own team
- **Spectator**: read-only

### Config

Bot token in K8s Secret, KIMO API URL + key in ConfigMap.

## Networking & Security

### Resource Limits
- Per-instance: pod resource requests/limits from template
- Per-team: ResourceQuota per team namespace
- Global: `maxInstances` on template + operator-level cap

### Security Hardening
- Non-root by default (overridable for pwn challenges)
- `SecurityContext`: readOnlyRootFilesystem, no_new_privs, dropped capabilities — paired with a writable `/tmp` (`emptyDir`) so the read-only root doesn't outright break ordinary images (confirmed live: stock `nginx` crash-loops on `mkdir /tmp/proxy_temp` without it)
- Pod Security Standards at namespace level (restricted default, privileged opt-in)
- Service account token not mounted in challenge pods
- TLS via cert-manager integration

## Project Structure

```
kimo/
├── cmd/
│   ├── manager/              # operator entrypoint
│   └── bot/                  # discord bot entrypoint
├── api/
│   └── v1alpha1/             # CRD types
├── internal/
│   ├── controller/
│   │   ├── template_controller.go
│   │   ├── instance_controller.go    # container lifecycle state machine
│   │   ├── network_controller.go
│   │   ├── lifecycle_controller.go
│   │   └── set_controller.go
│   ├── integrations/                 # pluggable scoring backend adapters
│   │   ├── backend.go                # interface + registry
│   │   ├── generic.go                # default HMAC-webhook adapter
│   │   ├── ctfd.go
│   │   └── rctf.go
│   ├── api/
│   │   ├── server.go
│   │   ├── handlers.go
│   │   └── pow.go
│   ├── bot/
│   │   ├── bot.go
│   │   ├── commands.go
│   │   ├── monitor.go
│   │   └── rbac.go
│   └── webhook/              # admission webhooks
├── config/
│   ├── crd/
│   ├── rbac/
│   ├── manager/
│   └── samples/
├── helm/
│   └── kimo/
├── test/
│   ├── e2e/
│   └── integration/
├── Dockerfile
├── Makefile
└── go.mod
```

## Testing Strategy

- **Unit**: Controller reconciliation with fake clients, including the Pod-health-driven phase transitions
- **Integration**: CRD lifecycle with envtest
- **E2E**: Full operator in kind cluster
- **API**: REST handlers with httptest, PoW verification
- **Backends**: each adapter unit-tested against recorded fixtures of its platform's expected payload/auth shape
- **CI**: GitHub Actions (lint, unit, integration, e2e)
