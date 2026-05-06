# KIMO Design Document

**Date**: 2026-04-10
**Status**: Approved

## Overview

KIMO (Kubernetes Instance Manager Operator) is a Go-based Kubernetes operator that deploys and manages CTF challenge instances on containers and VMs. It provides on-demand provisioning, automatic TTL-based cleanup, built-in network isolation, a REST API for scoring platform integration, PoW-based abuse prevention, and a Discord bot for organizer management.

## Requirements

- **Runtime**: Containers (Pods/Deployments) and VMs (KubeVirt)
- **Isolation**: Configurable per challenge (shared / per-team / per-player)
- **Language**: Go with kubebuilder/controller-runtime
- **Integration**: Generic REST API + webhooks for any scoring platform
- **Scale**: 500+ concurrent instances
- **Networking**: Built-in NetworkPolicy management
- **Lifecycle**: On-demand provisioning + configurable TTL auto-cleanup
- **PoW**: Proof of Work to prevent instance provisioning abuse
- **Discord**: Bot for challenge management and monitoring

## Architecture

Single operator binary with multiple controllers, plus a separate Discord bot binary. Both deployed via Helm in the `kimo-system` namespace.

```
                    +-----------------+
                    |  Scoring Platform|
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
+-------+-------+  +-------+--------+  +----------------+
        |                   |
   +----+----+         +----+----+
   |         |         |         |
 Pods    KubeVirt   NetPol    Ingress
         VMIs
```

## Custom Resource Definitions

API group: `kimo.io/v1alpha1`

### ChallengeTemplate

Blueprint for a challenge, created by challenge authors.

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
  runtime: container          # container | vm
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
  # vm: { ... }              # KubeVirt VMI spec when runtime=vm
status:
  ready: true
  instanceCount: 42
```

### ChallengeInstance

A running instance of a challenge.

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
  phase: Running              # Pending | Running | Expired | Failed
  endpoint: "https://web-sqli-101-team-42.ctf.example.com"
  startedAt: "2026-04-10T14:00:00Z"
  expiresAt: "2026-04-10T14:45:00Z"
  podName: web-sqli-101-team-42-xyz
```

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
    startAt: "2026-04-10T10:00:00Z"
    endAt: "2026-04-10T18:00:00Z"
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
- Validates ChallengeTemplate specs (image, resources, flag secret)
- Sets `status.ready` and tracks `status.instanceCount`
- Enforces `maxInstances` cap

### Instance Controller
- Creates workloads based on `runtime`:
  - Container: Deployment + Service
  - VM: KubeVirt VirtualMachineInstance + Service
- Injects per-instance flags
- Updates status (phase, endpoint, timestamps)
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
- Expires instances where `expiresAt < now`
- Handles TTL extensions
- Grace period before deletion

### Set Controller
- Manages bulk start/stop of challenge groups
- Enforces schedule windows

## REST API

Deployed as a Service in `kimo-system`, authenticated via API key (`Bearer` token).

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
| `POST` | `/api/v1/webhooks/configure` | Register webhook URL |
| `GET` | `/api/v1/health` | Health check |

### Webhook Callbacks

Events: `instance.pending`, `instance.running`, `instance.expired`, `instance.failed`, `instance.deleted`

```json
{
  "event": "instance.running",
  "instance": "web-sqli-101-team-42",
  "challenge": "web-sqli-101",
  "team": "team-42",
  "endpoint": "https://web-sqli-101-team-42.ctf.example.com",
  "timestamp": "2026-04-10T14:00:05Z"
}
```

Webhook signatures verified via HMAC-SHA256.

## Proof of Work

Configurable per ChallengeTemplate. Enforced by the API server before provisioning.

1. Client requests puzzle: `GET /api/v1/pow/challenge?template=web-sqli-101`
2. Server returns SHA-256 partial preimage challenge with difficulty and TTL
3. Client solves locally, submits nonce with provisioning request
4. Server verifies before creating ChallengeInstance CR

Difficulty tunable per challenge (harder for expensive VMs, easier for containers).

## Discord Bot

Separate binary (`cmd/bot/`), deployed alongside the operator. Communicates via KIMO REST API.

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

Registers as a KIMO webhook consumer. Posts formatted embeds to monitoring channels on instance status changes.

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
- `SecurityContext`: readOnlyRootFilesystem, no_new_privs, dropped capabilities
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
│   │   ├── instance_controller.go
│   │   ├── network_controller.go
│   │   ├── lifecycle_controller.go
│   │   └── set_controller.go
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

- **Unit**: Controller reconciliation with fake clients
- **Integration**: CRD lifecycle with envtest
- **E2E**: Full operator in kind cluster
- **API**: REST handlers with httptest, PoW verification
- **CI**: GitHub Actions (lint, unit, integration, e2e)
