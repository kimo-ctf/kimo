---

<p align="center">
  <img src="assets/kimo.png" alt="kimo logo" width="280"/>
</p>

<p align="center">
  <em>lightweight CTF instance manager</em>
  <br>
  <em>version 0.1.0</em>
</p>

---

## Overview

**kimo** manages on-demand VM instances for CTF challenges. It exposes a
small REST API that:

1. Issues a **Proof-of-Work (PoW)** challenge to rate-limit abuse.
2. Validates team credentials against the configured CTF platform.
3. Provisions a fresh VM via the selected backend.
4. Tracks the lifecycle of every instance.

The architecture is **fully pluggable**: both the CTF-platform layer and the
VM-backend layer are defined as Go interfaces. Adding a new platform or VM
backend requires only implementing the interface and registering the factory
with a single `init()` call.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                          HTTP API                           │
│  GET  /api/v1/pow/challenge                                 │
│  POST /api/v1/instances          (PoW required)             │
│  GET  /api/v1/instances[/{id}]                              │
│  DELETE /api/v1/instances/{id}   (PoW required)             │
└───────────────────────┬─────────────────────────────────────┘
                        │
             ┌──────────▼──────────┐
             │   Instance Manager  │
             └──┬──────────────┬───┘
                │              │
    ┌───────────▼──┐   ┌───────▼────────┐   ┌──────────────┐
    │  Platform    │   │  VM Provider   │   │  PoW Manager │
    │  Provider    │   │                │   │              │
    ├──────────────┤   ├────────────────┤   └──────────────┘
    │ ctfd         │   │ firecracker    │
    │ (+ more)     │   │ kubevirt       │
    └──────────────┘   │ (+ more)       │
                       └────────────────┘
```

### Extension points

| Layer            | Interface              | Register via                    |
|------------------|------------------------|---------------------------------|
| CTF platform     | `platform.Provider`    | `platform.Register(name, func)` |
| VM backend       | `vm.Provider`          | `vm.Register(name, func)`       |

Both interfaces live in `internal/platform/provider.go` and
`internal/vm/provider.go` respectively.

---

## Providers

### Platform providers

| Name    | Package                              | Description              |
|---------|--------------------------------------|--------------------------|
| `ctfd`  | `internal/platform/ctfd`             | CTFd REST API            |

### VM providers

| Name          | Package                        | Description                           |
|---------------|--------------------------------|---------------------------------------|
| `firecracker` | `internal/vm/firecracker`      | Firecracker microVMs (Unix socket API)|
| `kubevirt`    | `internal/vm/kubevirt`         | KubeVirt VirtualMachine CRDs          |

---

## Configuration

Copy `kimo.example.yaml` to `kimo.yaml` and fill in your values.

```yaml
server:
  host: "0.0.0.0"
  port: 8080

platform:
  type: ctfd          # registered provider name
  ctfd:
    url: "https://ctf.example.com"
    api_key: "YOUR_KEY"

vm:
  type: firecracker   # "firecracker" or "kubevirt"
  firecracker:
    binary_path: "firecracker"
    socket_dir: "/run/kimo/vms"
    kernel_image: "/var/lib/kimo/vmlinux"
    rootfs_path: "/var/lib/kimo/ubuntu-22.04.ext4"
  kubevirt:
    kubeconfig: "/etc/kimo/kubeconfig"
    namespace: "kimo"

pow:
  difficulty: 20   # leading zero bits in SHA-256(prefix:nonce)
  ttl: 5m          # challenge lifetime
```

---

## Proof-of-Work

Every instance creation and deletion requires a valid PoW solution:

1. **Fetch a challenge** – `GET /api/v1/pow/challenge`

   ```json
   {
     "id": "a3f1...",
     "prefix": "d7e2...",
     "difficulty": 20,
     "expires_at": "2024-01-01T00:05:00Z"
   }
   ```

2. **Solve the challenge** – iterate nonces until:
   `leading_zero_bits( SHA-256(prefix + ":" + nonce) ) >= difficulty`

   Example solver (Python):
   ```python
   import hashlib, itertools

   def solve(prefix, difficulty):
       for i in itertools.count():
           nonce = str(i)
           h = hashlib.sha256(f"{prefix}:{nonce}".encode()).digest()
           bits = int.from_bytes(h, 'big')
           if bits >> (256 - difficulty) == 0:
               return nonce
   ```

3. **Use the solution** in the instance request:

   ```json
   {
     "team_id": "42",
     "team_token": "...",
     "ctf_challenge_id": "web-01",
     "pow_challenge_id": "a3f1...",
     "pow_nonce": "173842",
     "vm_spec": { "image": "", "memory_mb": 512, "cpus": 1 }
   }
   ```

**Replay protection**: each challenge may only be consumed once.
**Expiry**: challenges expire after the configured TTL (default 5 minutes).

---

## API reference

| Method   | Path                       | Auth  | Description              |
|----------|----------------------------|-------|--------------------------|
| `GET`    | `/api/v1/pow/challenge`    | –     | Issue a new PoW challenge|
| `POST`   | `/api/v1/instances`        | PoW   | Create an instance       |
| `GET`    | `/api/v1/instances`        | –     | List all instances       |
| `GET`    | `/api/v1/instances/{id}`   | –     | Get instance details     |
| `DELETE` | `/api/v1/instances/{id}`   | PoW   | Delete an instance       |
| `GET`    | `/healthz`                 | –     | Health check             |

---

## Building & running

```bash
# Build
go build -o kimo ./cmd/kimo

# Start the server
./kimo serve -config kimo.yaml

# List registered providers (no config required)
./kimo providers

# Print version
./kimo version
```

---

## Adding a new provider

### New CTF platform

1. Create `internal/platform/<name>/provider.go`.
2. Implement `platform.Provider`.
3. Call `platform.Register("<name>", factory)` in `init()`.
4. Blank-import the package in `cmd/kimo/main.go`.
5. Add a config struct + case in `internal/config/config.go`.

### New VM backend

Same pattern but use `vm.Register` and `vm.Provider` from `internal/vm`.

---

## Running tests

```bash
go test ./...
```
