# KIMO Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:executing-plans to implement this plan task-by-task.

**Goal:** Build a Kubernetes operator that deploys and manages CTF challenge instances as containers, with a health-driven lifecycle state machine, a REST API, PoW protection, a pluggable scoring-backend integration layer, and a Discord bot.

**Architecture:** Single Go operator binary (kubebuilder) with 5 controllers (Template, Instance, Network, Lifecycle, Set), a REST API server embedded in the manager, a pluggable `Backend` integration layer (`internal/integrations/`) with a generic webhook adapter and platform-specific adapters (CTFd shown as the worked example), and a separate Discord bot binary. All deployed via Helm.

**Revision note:** this plan supersedes the original version, which included a KubeVirt VM runtime and a single hard-coded generic-webhook integration. VM support is dropped entirely — KIMO is container-only. Integration is now a proper plugin seam (`Backend` interface + registry) instead of code baked into the API server, and the Instance Controller drives a real Pod-health state machine instead of only tracking TTLs.

**Tech Stack:** Go 1.22+, kubebuilder v4, controller-runtime, chi (HTTP router), discordgo, envtest, kind

---

### Task 0: Project Scaffold

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `cmd/manager/main.go`
- Create: `Makefile`
- Create: `Dockerfile`
- Create: `.gitignore`

**Step 1: Initialize kubebuilder project**

```bash
# Install kubebuilder if not present
go install sigs.k8s.io/kubebuilder/cmd/kubebuilder@latest

# Initialize project
kubebuilder init --domain kimo.io --repo github.com/hermannchristopher/kimo
```

This generates `cmd/manager/main.go`, `Makefile`, `Dockerfile`, `go.mod`, `config/` directories.

**Step 2: Verify scaffold builds**

```bash
make build
```

Expected: binary compiles with no errors.

**Step 3: Create .gitignore**

```gitignore
bin/
testbin/
cover.out
*.test
```

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: scaffold kubebuilder project"
```

---

### Task 1: CRD Types — ChallengeTemplate

**Files:**
- Create: `api/v1alpha1/challengetemplate_types.go`
- Create: `api/v1alpha1/groupversion_info.go`
- Create: `api/v1alpha1/zz_generated.deepcopy.go` (generated)

**Step 1: Create the API with kubebuilder**

```bash
kubebuilder create api --group kimo --version v1alpha1 --kind ChallengeTemplate --resource --controller
```

**Step 2: Define ChallengeTemplate spec and status types**

Containers are the only runtime — there is no runtime switch and no VM spec. `ContainerSpec` gains the fields that drive the Instance Controller's lifecycle state machine: `Readiness`, `RestartPolicy`, `UnhealthyThreshold`.

Edit `api/v1alpha1/challengetemplate_types.go`:

```go
package v1alpha1

import (
        corev1 "k8s.io/api/core/v1"
        "k8s.io/apimachinery/pkg/api/resource"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstanceMode defines how challenge instances are scoped.
// +kubebuilder:validation:Enum=shared;perTeam;perPlayer
type InstanceMode string

const (
        InstanceModeShared    InstanceMode = "shared"
        InstanceModePerTeam   InstanceMode = "perTeam"
        InstanceModePerPlayer InstanceMode = "perPlayer"
)

// PoWSpec configures Proof of Work for a challenge.
type PoWSpec struct {
        Enabled    bool   `json:"enabled"`
        Difficulty int    `json:"difficulty"`            // leading zero bits
        Algorithm  string `json:"algorithm,omitempty"`   // sha256 (default)
        TTL        string `json:"ttl,omitempty"`         // puzzle expiry, e.g. "5m"
}

// ContainerPort defines an exposed port.
type ContainerPort struct {
        Name          string `json:"name"`
        ContainerPort int32  `json:"containerPort"`
        Expose        bool   `json:"expose,omitempty"`
}

// ResourceRequirements mirrors k8s resource requests/limits.
type ResourceRequirements struct {
        Requests map[corev1.ResourceName]resource.Quantity `json:"requests,omitempty"`
        Limits   map[corev1.ResourceName]resource.Quantity `json:"limits,omitempty"`
}

// ReadinessType selects how the Instance Controller checks container readiness.
// +kubebuilder:validation:Enum=tcp;http;none
type ReadinessType string

const (
        ReadinessTCP  ReadinessType = "tcp"
        ReadinessHTTP ReadinessType = "http"
        ReadinessNone ReadinessType = "none"
)

// ReadinessCheck configures the Pending/Creating -> Running transition.
// Defaults to a TCP check on the first exposed port when omitted.
type ReadinessCheck struct {
        Type ReadinessType `json:"type,omitempty"`
        Port int32         `json:"port,omitempty"`
        Path string        `json:"path,omitempty"` // http only
}

// RestartPolicy mirrors a subset of corev1.RestartPolicy.
// +kubebuilder:validation:Enum=OnFailure;Always;Never
type RestartPolicy string

const (
        RestartOnFailure RestartPolicy = "OnFailure"
        RestartAlways    RestartPolicy = "Always"
        RestartNever     RestartPolicy = "Never"
)

// ContainerSpec defines the container runtime configuration.
type ContainerSpec struct {
        Image              string               `json:"image"`
        Ports              []ContainerPort      `json:"ports,omitempty"`
        Resources          ResourceRequirements `json:"resources,omitempty"`
        Env                []corev1.EnvVar      `json:"env,omitempty"`
        Readiness          *ReadinessCheck      `json:"readiness,omitempty"`
        RestartPolicy      RestartPolicy        `json:"restartPolicy,omitempty"`      // default Always; only Always is accepted today (see Task 4)
        UnhealthyThreshold int32                `json:"unhealthyThreshold,omitempty"` // default 3
}

// ChallengeTemplateSpec defines the desired state of ChallengeTemplate.
type ChallengeTemplateSpec struct {
        Category      string                      `json:"category,omitempty"`
        Difficulty    string                      `json:"difficulty,omitempty"`
        Points        int                         `json:"points,omitempty"`
        FlagSecretRef corev1.LocalObjectReference `json:"flagSecretRef"`
        InstanceMode  InstanceMode                `json:"instanceMode"`
        TTL           string                      `json:"ttl"` // e.g. "30m"
        MaxInstances  int                         `json:"maxInstances"`
        PoW           *PoWSpec                    `json:"pow,omitempty"`
        Container     ContainerSpec               `json:"container"`
}

// ChallengeTemplateStatus defines the observed state.
type ChallengeTemplateStatus struct {
        Ready         bool   `json:"ready"`
        InstanceCount int    `json:"instanceCount"`
        Message       string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.instanceMode`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.status.instanceCount`
type ChallengeTemplate struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`
        Spec              ChallengeTemplateSpec   `json:"spec,omitempty"`
        Status            ChallengeTemplateStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ChallengeTemplateList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []ChallengeTemplate `json:"items"`
}

func init() {
        SchemeBuilder.Register(&ChallengeTemplate{}, &ChallengeTemplateList{})
}
```

**Step 3: Generate deepcopy and CRD manifests**

```bash
make generate
make manifests
```

Expected: `zz_generated.deepcopy.go` and `config/crd/bases/kimo.kimo.io_challengetemplates.yaml` created.

**Step 4: Verify it compiles**

```bash
make build
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add ChallengeTemplate CRD types"
```

---

### Task 2: CRD Types — ChallengeInstance

**Files:**
- Create: `api/v1alpha1/challengeinstance_types.go`

**Step 1: Create the API**

```bash
kubebuilder create api --group kimo --version v1alpha1 --kind ChallengeInstance --resource --controller
```

**Step 2: Define ChallengeInstance types**

The phase enum now models real container health, not just TTL bookkeeping: `Pending -> Creating -> Running <-> Unhealthy -> Expiring -> Expired -> Terminating`, with `Failed` reachable from any non-terminal phase.

Edit `api/v1alpha1/challengeinstance_types.go`:

```go
package v1alpha1

import (
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstancePhase represents the lifecycle phase of an instance.
// +kubebuilder:validation:Enum=Pending;Creating;Running;Unhealthy;Expiring;Expired;Terminating;Failed
type InstancePhase string

const (
        InstancePhasePending     InstancePhase = "Pending"
        InstancePhaseCreating    InstancePhase = "Creating"
        InstancePhaseRunning     InstancePhase = "Running"
        InstancePhaseUnhealthy   InstancePhase = "Unhealthy"
        InstancePhaseExpiring    InstancePhase = "Expiring"
        InstancePhaseExpired     InstancePhase = "Expired"
        InstancePhaseTerminating InstancePhase = "Terminating"
        InstancePhaseFailed      InstancePhase = "Failed"
)

// ChallengeInstanceSpec defines the desired state.
type ChallengeInstanceSpec struct {
        TemplateRef string `json:"templateRef"`
        Team        string `json:"team"`
        Player      string `json:"player,omitempty"`
        TTLOverride string `json:"ttlOverride,omitempty"` // e.g. "45m"
}

// ChallengeInstanceStatus defines the observed state.
type ChallengeInstanceStatus struct {
        Phase          InstancePhase `json:"phase,omitempty"`
        Reason         string        `json:"reason,omitempty"`
        Endpoint       string        `json:"endpoint,omitempty"`
        StartedAt      *metav1.Time  `json:"startedAt,omitempty"`
        ExpiresAt      *metav1.Time  `json:"expiresAt,omitempty"`
        PodName        string        `json:"podName,omitempty"`
        UnhealthyCount int32         `json:"unhealthyCount,omitempty"` // consecutive failed readiness checks
        Message        string        `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Challenge",type=string,JSONPath=`.spec.templateRef`
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=`.spec.team`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.expiresAt`
type ChallengeInstance struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`
        Spec              ChallengeInstanceSpec   `json:"spec,omitempty"`
        Status            ChallengeInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ChallengeInstanceList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []ChallengeInstance `json:"items"`
}

func init() {
        SchemeBuilder.Register(&ChallengeInstance{}, &ChallengeInstanceList{})
}
```

**Step 3: Generate and build**

```bash
make generate && make manifests && make build
```

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: add ChallengeInstance CRD types"
```

---

### Task 3: CRD Types — ChallengeSet and NetworkFence

**Files:**
- Create: `api/v1alpha1/challengeset_types.go`
- Create: `api/v1alpha1/networkfence_types.go`

**Step 1: Create both APIs**

```bash
kubebuilder create api --group kimo --version v1alpha1 --kind ChallengeSet --resource --controller
kubebuilder create api --group kimo --version v1alpha1 --kind NetworkFence --resource --controller
```

**Step 2: Define ChallengeSet types**

Edit `api/v1alpha1/challengeset_types.go`:

```go
package v1alpha1

import (
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ScheduleSpec struct {
        StartAt string `json:"startAt,omitempty"` // RFC3339
        EndAt   string `json:"endAt,omitempty"`   // RFC3339
}

type ChallengeSetSpec struct {
        Challenges []string      `json:"challenges"`
        Schedule   *ScheduleSpec `json:"schedule,omitempty"`
}

type ChallengeSetStatus struct {
        Active  bool   `json:"active"`
        Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Active",type=boolean,JSONPath=`.status.active`
type ChallengeSet struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`
        Spec              ChallengeSetSpec   `json:"spec,omitempty"`
        Status            ChallengeSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ChallengeSetList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []ChallengeSet `json:"items"`
}

func init() {
        SchemeBuilder.Register(&ChallengeSet{}, &ChallengeSetList{})
}
```

**Step 3: Define NetworkFence types**

Edit `api/v1alpha1/networkfence_types.go`:

```go
package v1alpha1

import (
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AllowRule struct {
        CIDR string `json:"cidr,omitempty"`
        Port int32  `json:"port,omitempty"`
}

type DenyRule struct {
        To string `json:"to,omitempty"` // namespace name to block
}

type NetworkFenceSpec struct {
        InstanceRef string      `json:"instanceRef"`
        AllowRules  []AllowRule `json:"allow,omitempty"`
        DenyRules   []DenyRule  `json:"deny,omitempty"`
        AllowEgress bool        `json:"allowEgress,omitempty"` // allow internet
}

type NetworkFenceStatus struct {
        Applied bool   `json:"applied"`
        Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=`.spec.instanceRef`
// +kubebuilder:printcolumn:name="Applied",type=boolean,JSONPath=`.status.applied`
type NetworkFence struct {
        metav1.TypeMeta   `json:",inline"`
        metav1.ObjectMeta `json:"metadata,omitempty"`
        Spec              NetworkFenceSpec   `json:"spec,omitempty"`
        Status            NetworkFenceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type NetworkFenceList struct {
        metav1.TypeMeta `json:",inline"`
        metav1.ListMeta `json:"metadata,omitempty"`
        Items           []NetworkFence `json:"items"`
}

func init() {
        SchemeBuilder.Register(&NetworkFence{}, &NetworkFenceList{})
}
```

**Step 4: Generate and build**

```bash
make generate && make manifests && make build
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add ChallengeSet and NetworkFence CRD types"
```

---

### Task 4: Template Controller

**Files:**
- Modify: `internal/controller/challengetemplate_controller.go`
- Create: `internal/controller/challengetemplate_controller_test.go`

**Step 1: Write the failing test**

Create `internal/controller/challengetemplate_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        corev1 "k8s.io/api/core/v1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/types"
        "sigs.k8s.io/controller-runtime/pkg/client/fake"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
        "k8s.io/apimachinery/pkg/runtime"
)

func TestTemplateController_ReconcileValid(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))
        require.NoError(t, corev1.AddToScheme(scheme))

        secret := &corev1.Secret{
                ObjectMeta: metav1.ObjectMeta{Name: "flag-secret", Namespace: "default"},
                Data:       map[string][]byte{"flag": []byte("FLAG{test}")},
        }

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "test-challenge", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        FlagSecretRef: corev1.LocalObjectReference{Name: "flag-secret"},
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container: kimov1alpha1.ContainerSpec{
                                Image: "test:latest",
                        },
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(secret, tmpl).
                WithStatusSubresource(tmpl).
                Build()

        r := &ChallengeTemplateReconciler{Client: client, Scheme: scheme}
        result, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "test-challenge", Namespace: "default"},
        })

        require.NoError(t, err)
        assert.Equal(t, reconcile.Result{RequeueAfter: 30 * time.Second}, result)

        var updated kimov1alpha1.ChallengeTemplate
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "test-challenge", Namespace: "default"}, &updated))
        assert.True(t, updated.Status.Ready)
}

func TestTemplateController_MissingSecret(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))
        require.NoError(t, corev1.AddToScheme(scheme))

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "test-challenge", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        FlagSecretRef: corev1.LocalObjectReference{Name: "missing-secret"},
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container:     kimov1alpha1.ContainerSpec{Image: "test:latest"},
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl).
                WithStatusSubresource(tmpl).
                Build()

        r := &ChallengeTemplateReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "test-challenge", Namespace: "default"},
        })

        require.NoError(t, err) // reconcile doesn't error, sets status

        var updated kimov1alpha1.ChallengeTemplate
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "test-challenge", Namespace: "default"}, &updated))
        assert.False(t, updated.Status.Ready)
        assert.Contains(t, updated.Status.Message, "flag secret")
}

func TestTemplateController_MissingImage(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))
        require.NoError(t, corev1.AddToScheme(scheme))

        secret := &corev1.Secret{
                ObjectMeta: metav1.ObjectMeta{Name: "flag-secret", Namespace: "default"},
                Data:       map[string][]byte{"flag": []byte("FLAG{test}")},
        }

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "test-challenge", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        FlagSecretRef: corev1.LocalObjectReference{Name: "flag-secret"},
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        TTL:           "30m",
                        MaxInstances:  100,
                        // Container.Image left empty — should fail validation
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(secret, tmpl).
                WithStatusSubresource(tmpl).
                Build()

        r := &ChallengeTemplateReconciler{Client: client, Scheme: scheme}
        r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "test-challenge", Namespace: "default"},
        })

        var updated kimov1alpha1.ChallengeTemplate
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "test-challenge", Namespace: "default"}, &updated))
        assert.False(t, updated.Status.Ready)
        assert.Contains(t, updated.Status.Message, "image")
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/controller/ -run TestTemplateController -v
```

Expected: compilation error (reconciler not implemented yet).

**Step 3: Implement the Template Controller**

Edit `internal/controller/challengetemplate_controller.go`:

```go
package controller

import (
        "context"
        "fmt"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        corev1 "k8s.io/api/core/v1"
        "k8s.io/apimachinery/pkg/api/errors"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        ctrl "sigs.k8s.io/controller-runtime"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/log"
)

type ChallengeTemplateReconciler struct {
        client.Client
        Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ChallengeTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
        logger := log.FromContext(ctx)

        var tmpl kimov1alpha1.ChallengeTemplate
        if err := r.Get(ctx, req.NamespacedName, &tmpl); err != nil {
                if errors.IsNotFound(err) {
                        return ctrl.Result{}, nil
                }
                return ctrl.Result{}, err
        }

        // Validate flag secret exists
        var secret corev1.Secret
        secretKey := types.NamespacedName{
                Name:      tmpl.Spec.FlagSecretRef.Name,
                Namespace: tmpl.Namespace,
        }
        if err := r.Get(ctx, secretKey, &secret); err != nil {
                if errors.IsNotFound(err) {
                        return r.setStatus(ctx, &tmpl, false, "flag secret not found: "+tmpl.Spec.FlagSecretRef.Name)
                }
                return ctrl.Result{}, err
        }

        // Validate container spec
        if tmpl.Spec.Container.Image == "" {
                return r.setStatus(ctx, &tmpl, false, "container image is required")
        }
        // Instances are Deployment-managed, and Kubernetes only allows
        // RestartPolicy: Always on Deployment-managed pods — OnFailure/Never
        // require a bare Pod, which isn't supported yet. Reject early here
        // instead of letting Deployment creation fail in the Instance
        // Controller (found by running the built image against a real
        // cluster: a template with restartPolicy: OnFailure — the design
        // doc's own original example — crash-loops the reconciler).
        switch tmpl.Spec.Container.RestartPolicy {
        case "", kimov1alpha1.RestartAlways:
        default:
                return r.setStatus(ctx, &tmpl, false, "container.restartPolicy: only \"Always\" (or omitted) is supported — instances are Deployment-managed and Kubernetes requires Always for those; OnFailure/Never would need bare-Pod support")
        }

        // Count existing instances
        var instances kimov1alpha1.ChallengeInstanceList
        if err := r.List(ctx, &instances, client.MatchingLabels{
                "kimo.io/challenge": tmpl.Name,
        }); err != nil {
                return ctrl.Result{}, err
        }

        tmpl.Status.InstanceCount = len(instances.Items)
        tmpl.Status.Ready = true
        tmpl.Status.Message = ""

        if err := r.Status().Update(ctx, &tmpl); err != nil {
                return ctrl.Result{}, err
        }

        logger.Info("template reconciled", "name", tmpl.Name, "ready", true, "instances", tmpl.Status.InstanceCount)
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ChallengeTemplateReconciler) setStatus(ctx context.Context, tmpl *kimov1alpha1.ChallengeTemplate, ready bool, msg string) (ctrl.Result, error) {
        tmpl.Status.Ready = ready
        tmpl.Status.Message = msg
        if err := r.Status().Update(ctx, tmpl); err != nil {
                return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
        }
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *ChallengeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
                For(&kimov1alpha1.ChallengeTemplate{}).
                Complete(r)
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/controller/ -run TestTemplateController -v
```

Expected: all 3 tests PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement Template Controller with validation"
```

---

### Task 5: Instance Controller — Container Lifecycle

**Files:**
- Modify: `internal/controller/challengeinstance_controller.go`
- Create: `internal/controller/challengeinstance_controller_test.go`

This is the controller that owns the container lifecycle state machine described in the design doc. It creates the Deployment/Service, then watches the owned Pod's status to drive `Pending -> Creating -> Running <-> Unhealthy`. It depends on `internal/integrations.Backend` (Task 6) to notify the active scoring backend on every transition — define the reconciler with a `Backend` field now and wire a no-op test double until Task 6 lands.

**Step 1: Write the failing test**

Create `internal/controller/challengeinstance_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/hermannchristopher/kimo/internal/integrations"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        appsv1 "k8s.io/api/apps/v1"
        corev1 "k8s.io/api/core/v1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "sigs.k8s.io/controller-runtime/pkg/client/fake"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
        t.Helper()
        s := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(s))
        require.NoError(t, corev1.AddToScheme(s))
        require.NoError(t, appsv1.AddToScheme(s))
        return s
}

// recordingBackend is a test double that records every Notify call.
type recordingBackend struct {
        events []integrations.Event
}

func (b *recordingBackend) Name() string { return "recording" }
func (b *recordingBackend) Notify(_ context.Context, e integrations.Event) error {
        b.events = append(b.events, e)
        return nil
}
func (b *recordingBackend) Authenticate(_ *http.Request) (integrations.Principal, error) {
        return integrations.Principal{}, nil
}

func newTemplate() *kimov1alpha1.ChallengeTemplate {
        return &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "web-sqli", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        FlagSecretRef: corev1.LocalObjectReference{Name: "flag"},
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container: kimov1alpha1.ContainerSpec{
                                Image: "ctf/web-sqli:v1",
                                Ports: []kimov1alpha1.ContainerPort{
                                        {Name: "http", ContainerPort: 8080, Expose: true},
                                },
                                UnhealthyThreshold: 3,
                        },
                },
                Status: kimov1alpha1.ChallengeTemplateStatus{Ready: true},
        }
}

func newInstance() *kimov1alpha1.ChallengeInstance {
        return &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{
                        Name: "web-sqli-team-1", Namespace: "default",
                        Labels: map[string]string{"kimo.io/challenge": "web-sqli", "kimo.io/team": "team-1"},
                },
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "web-sqli",
                        Team:        "team-1",
                },
        }
}

func TestInstanceController_CreatesDeploymentAndService(t *testing.T) {
        scheme := newTestScheme(t)
        tmpl, instance := newTemplate(), newInstance()

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance).
                WithStatusSubresource(instance).
                Build()

        backend := &recordingBackend{}
        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme, Backend: backend}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        var dep appsv1.Deployment
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &dep))
        assert.Equal(t, "ctf/web-sqli:v1", dep.Spec.Template.Spec.Containers[0].Image)

        var svc corev1.Service
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &svc))
        assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseCreating, updated.Status.Phase)
        assert.NotNil(t, updated.Status.ExpiresAt)

        require.Len(t, backend.events, 1)
        assert.Equal(t, integrations.EventCreating, backend.events[0].Type)
}

func TestInstanceController_PodReadyTransitionsToRunning(t *testing.T) {
        scheme := newTestScheme(t)
        tmpl, instance := newTemplate(), newInstance()
        instance.Status.Phase = kimov1alpha1.InstancePhaseCreating

        pod := &corev1.Pod{
                ObjectMeta: metav1.ObjectMeta{
                        Name: "web-sqli-team-1-abcde", Namespace: "default",
                        Labels: map[string]string{"kimo.io/instance": "web-sqli-team-1"},
                },
                Status: corev1.PodStatus{
                        Phase: corev1.PodRunning,
                        Conditions: []corev1.PodCondition{
                                {Type: corev1.PodReady, Status: corev1.ConditionTrue},
                        },
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance, pod).
                WithStatusSubresource(instance).
                Build()

        backend := &recordingBackend{}
        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme, Backend: backend}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseRunning, updated.Status.Phase)

        require.Len(t, backend.events, 1)
        assert.Equal(t, integrations.EventRunning, backend.events[0].Type)
}

func TestInstanceController_UnreadyPodBeyondThresholdGoesUnhealthy(t *testing.T) {
        scheme := newTestScheme(t)
        tmpl, instance := newTemplate(), newInstance()
        instance.Status.Phase = kimov1alpha1.InstancePhaseRunning
        instance.Status.UnhealthyCount = 2 // one more failure crosses the threshold of 3

        pod := &corev1.Pod{
                ObjectMeta: metav1.ObjectMeta{
                        Name: "web-sqli-team-1-abcde", Namespace: "default",
                        Labels: map[string]string{"kimo.io/instance": "web-sqli-team-1"},
                },
                Status: corev1.PodStatus{
                        Phase: corev1.PodRunning,
                        Conditions: []corev1.PodCondition{
                                {Type: corev1.PodReady, Status: corev1.ConditionFalse},
                        },
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance, pod).
                WithStatusSubresource(instance).
                Build()

        backend := &recordingBackend{}
        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme, Backend: backend}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseUnhealthy, updated.Status.Phase)

        require.Len(t, backend.events, 1)
        assert.Equal(t, integrations.EventUnhealthy, backend.events[0].Type)
}

func TestInstanceController_TemplateNotReady(t *testing.T) {
        scheme := newTestScheme(t)

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "default"},
                Status:     kimov1alpha1.ChallengeTemplateStatus{Ready: false},
        }
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "broken-team-1", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "broken",
                        Team:        "team-1",
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance).
                WithStatusSubresource(instance).
                Build()

        backend := &recordingBackend{}
        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme, Backend: backend}
        result, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "broken-team-1", Namespace: "default"},
        })
        require.NoError(t, err)
        assert.NotZero(t, result.RequeueAfter)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "broken-team-1", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhasePending, updated.Status.Phase)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/controller/ -run TestInstanceController -v
```

**Step 3: Implement the Instance Controller**

Edit `internal/controller/challengeinstance_controller.go`:

```go
package controller

import (
        "context"
        "fmt"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/hermannchristopher/kimo/internal/integrations"
        appsv1 "k8s.io/api/apps/v1"
        corev1 "k8s.io/api/core/v1"
        "k8s.io/apimachinery/pkg/api/errors"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "k8s.io/apimachinery/pkg/util/intstr"
        ctrl "sigs.k8s.io/controller-runtime"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
        "sigs.k8s.io/controller-runtime/pkg/log"
)

type ChallengeInstanceReconciler struct {
        client.Client
        Scheme  *runtime.Scheme
        Backend integrations.Backend
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;pods,verbs=get;list;watch;create;update;patch;delete

func (r *ChallengeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
        logger := log.FromContext(ctx)

        var instance kimov1alpha1.ChallengeInstance
        if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
                if errors.IsNotFound(err) {
                        return ctrl.Result{}, nil
                }
                return ctrl.Result{}, err
        }

        if terminal(instance.Status.Phase) {
                return ctrl.Result{}, nil
        }

        var tmpl kimov1alpha1.ChallengeTemplate
        if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.TemplateRef, Namespace: instance.Namespace}, &tmpl); err != nil {
                if errors.IsNotFound(err) {
                        return r.transition(ctx, &instance, kimov1alpha1.InstancePhaseFailed, "template not found: "+instance.Spec.TemplateRef)
                }
                return ctrl.Result{}, err
        }

        if !tmpl.Status.Ready {
                r.transition(ctx, &instance, kimov1alpha1.InstancePhasePending, "waiting for template to be ready")
                return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
        }

        if err := r.ensureWorkload(ctx, &instance, &tmpl); err != nil {
                return ctrl.Result{}, err
        }

        if err := r.ensureExpiry(&instance, &tmpl); err != nil {
                return r.transition(ctx, &instance, kimov1alpha1.InstancePhaseFailed, err.Error())
        }

        phase, reason, err := r.determinePhase(ctx, &instance, &tmpl)
        if err != nil {
                return ctrl.Result{}, err
        }

        logger.Info("instance reconciled", "name", instance.Name, "phase", phase)
        return r.transition(ctx, &instance, phase, reason)
}

func terminal(p kimov1alpha1.InstancePhase) bool {
        return p == kimov1alpha1.InstancePhaseExpired || p == kimov1alpha1.InstancePhaseFailed
}

func (r *ChallengeInstanceReconciler) ensureWorkload(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) error {
        dep := r.buildDeployment(instance, tmpl)
        if err := controllerutil.SetControllerReference(instance, dep, r.Scheme); err != nil {
                return fmt.Errorf("setting owner reference: %w", err)
        }
        var existing appsv1.Deployment
        if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &existing); err != nil {
                if !errors.IsNotFound(err) {
                        return err
                }
                if err := r.Create(ctx, dep); err != nil {
                        return fmt.Errorf("creating deployment: %w", err)
                }
        }

        if svc := r.buildService(instance, tmpl); svc != nil {
                if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
                        return err
                }
                var existingSvc corev1.Service
                if err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &existingSvc); err != nil {
                        if !errors.IsNotFound(err) {
                                return err
                        }
                        if err := r.Create(ctx, svc); err != nil {
                                return fmt.Errorf("creating service: %w", err)
                        }
                }
        }

        instance.Status.PodName = dep.Name
        return nil
}

func (r *ChallengeInstanceReconciler) ensureExpiry(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) error {
        if instance.Status.ExpiresAt != nil {
                return nil
        }
        ttl := tmpl.Spec.TTL
        if instance.Spec.TTLOverride != "" {
                ttl = instance.Spec.TTLOverride
        }
        duration, err := time.ParseDuration(ttl)
        if err != nil {
                return fmt.Errorf("invalid TTL: %s", ttl)
        }
        now := metav1.Now()
        instance.Status.StartedAt = &now
        expiresAt := metav1.NewTime(now.Add(duration))
        instance.Status.ExpiresAt = &expiresAt
        return nil
}

// determinePhase inspects the owned Pod's status to drive the lifecycle
// state machine: no pod yet -> Creating; pod ready -> Running; pod running
// but failing readiness past the template's threshold -> Unhealthy.
func (r *ChallengeInstanceReconciler) determinePhase(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) (kimov1alpha1.InstancePhase, string, error) {
        var pods corev1.PodList
        if err := r.List(ctx, &pods, client.InNamespace(instance.Namespace),
                client.MatchingLabels{"kimo.io/instance": instance.Name}); err != nil {
                return "", "", err
        }
        if len(pods.Items) == 0 {
                return kimov1alpha1.InstancePhaseCreating, "waiting for pod to be scheduled", nil
        }

        pod := pods.Items[0]
        switch pod.Status.Phase {
        case corev1.PodFailed:
                return kimov1alpha1.InstancePhaseFailed, "pod failed: " + pod.Status.Reason, nil
        case corev1.PodRunning:
                if podReady(&pod) {
                        instance.Status.UnhealthyCount = 0
                        return kimov1alpha1.InstancePhaseRunning, "", nil
                }
                threshold := tmpl.Spec.Container.UnhealthyThreshold
                if threshold == 0 {
                        threshold = 3
                }
                instance.Status.UnhealthyCount++
                if instance.Status.UnhealthyCount >= threshold {
                        return kimov1alpha1.InstancePhaseUnhealthy, "pod failing readiness checks", nil
                }
                return kimov1alpha1.InstancePhaseCreating, "waiting for pod to become ready", nil
        default:
                return kimov1alpha1.InstancePhaseCreating, "waiting for pod to start", nil
        }
}

func podReady(pod *corev1.Pod) bool {
        for _, c := range pod.Status.Conditions {
                if c.Type == corev1.PodReady {
                        return c.Status == corev1.ConditionTrue
                }
        }
        return false
}

// transition updates status and, on an actual phase change, notifies the
// active scoring backend.
func (r *ChallengeInstanceReconciler) transition(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, phase kimov1alpha1.InstancePhase, reason string) (ctrl.Result, error) {
        changed := instance.Status.Phase != phase
        instance.Status.Phase = phase
        instance.Status.Reason = reason

        if err := r.Status().Update(ctx, instance); err != nil {
                return ctrl.Result{}, err
        }

        if changed && r.Backend != nil {
                if evt, ok := eventForPhase(phase); ok {
                        _ = r.Backend.Notify(ctx, integrations.Event{
                                Type:      evt,
                                Instance:  instance.Name,
                                Challenge: instance.Spec.TemplateRef,
                                Team:      instance.Spec.Team,
                                Player:    instance.Spec.Player,
                                Endpoint:  instance.Status.Endpoint,
                                Reason:    reason,
                        })
                }
        }

        return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func eventForPhase(phase kimov1alpha1.InstancePhase) (integrations.EventType, bool) {
        switch phase {
        case kimov1alpha1.InstancePhaseCreating:
                return integrations.EventCreating, true
        case kimov1alpha1.InstancePhaseRunning:
                return integrations.EventRunning, true
        case kimov1alpha1.InstancePhaseUnhealthy:
                return integrations.EventUnhealthy, true
        case kimov1alpha1.InstancePhaseFailed:
                return integrations.EventFailed, true
        default:
                return "", false
        }
}

func (r *ChallengeInstanceReconciler) buildDeployment(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *appsv1.Deployment {
        replicas := int32(1)
        labels := map[string]string{
                "kimo.io/challenge": tmpl.Name,
                "kimo.io/team":      instance.Spec.Team,
                "kimo.io/instance":  instance.Name,
        }

        c := tmpl.Spec.Container
        container := corev1.Container{
                Name:  "challenge",
                Image: c.Image,
                Env:   c.Env,
                SecurityContext: &corev1.SecurityContext{
                        RunAsNonRoot:             boolPtr(true),
                        ReadOnlyRootFilesystem:   boolPtr(true),
                        AllowPrivilegeEscalation: boolPtr(false),
                },
                // ReadOnlyRootFilesystem breaks most off-the-shelf images
                // outright — e.g. nginx crashes on startup trying to mkdir
                // under /tmp (confirmed by actually running this against a
                // kind cluster). A writable /tmp backed by emptyDir is the
                // standard middle ground: root filesystem stays read-only,
                // but scratch space still works without every challenge
                // author having to build a specially hardened image.
                VolumeMounts: []corev1.VolumeMount{
                        {Name: "tmp", MountPath: "/tmp"},
                },
        }
        for _, p := range c.Ports {
                container.Ports = append(container.Ports, corev1.ContainerPort{Name: p.Name, ContainerPort: p.ContainerPort})
        }
        if probe := buildReadinessProbe(c); probe != nil {
                container.ReadinessProbe = probe
        }

        // Kubernetes requires RestartPolicy: Always for any Deployment-managed
        // pod template — OnFailure/Never are only valid on bare Pods. Instances
        // are Deployment-managed (for self-healing + ownership), so
        // c.RestartPolicy is not honored here; the Template Controller (Task 4)
        // rejects OnFailure/Never at admission time instead of letting this
        // fail at Deployment-creation time.
        return &appsv1.Deployment{
                ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Labels: labels},
                Spec: appsv1.DeploymentSpec{
                        Replicas: &replicas,
                        Selector: &metav1.LabelSelector{MatchLabels: labels},
                        Template: corev1.PodTemplateSpec{
                                ObjectMeta: metav1.ObjectMeta{Labels: labels},
                                Spec: corev1.PodSpec{
                                        Containers:                   []corev1.Container{container},
                                        RestartPolicy:                corev1.RestartPolicyAlways,
                                        AutomountServiceAccountToken: boolPtr(false),
                                        Volumes: []corev1.Volume{
                                                {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
                                        },
                                },
                        },
                },
        }
}

func buildReadinessProbe(c kimov1alpha1.ContainerSpec) *corev1.Probe {
        readiness := c.Readiness
        if readiness != nil && readiness.Type == kimov1alpha1.ReadinessNone {
                return nil
        }

        port := int32(0)
        if readiness != nil && readiness.Port != 0 {
                port = readiness.Port
        } else if len(c.Ports) > 0 {
                port = c.Ports[0].ContainerPort
        }
        if port == 0 {
                return nil
        }

        if readiness != nil && readiness.Type == kimov1alpha1.ReadinessHTTP {
                return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
                        HTTPGet: &corev1.HTTPGetAction{Path: readiness.Path, Port: intstr.FromInt32(port)},
                }}
        }
        return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
                TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
        }}
}

func (r *ChallengeInstanceReconciler) buildService(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *corev1.Service {
        var ports []corev1.ServicePort
        for _, p := range tmpl.Spec.Container.Ports {
                if p.Expose {
                        ports = append(ports, corev1.ServicePort{Name: p.Name, Port: p.ContainerPort, TargetPort: intstr.FromInt32(p.ContainerPort)})
                }
        }
        if len(ports) == 0 {
                return nil
        }
        return &corev1.Service{
                ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
                Spec: corev1.ServiceSpec{
                        Selector: map[string]string{"kimo.io/instance": instance.Name},
                        Ports:    ports,
                },
        }
}

func boolPtr(b bool) *bool { return &b }

func (r *ChallengeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
                For(&kimov1alpha1.ChallengeInstance{}).
                Owns(&appsv1.Deployment{}).
                Owns(&corev1.Service{}).
                // The workload Pod isn't owned by the ChallengeInstance
                // directly — it's two hops away (Deployment -> ReplicaSet ->
                // Pod) — so Owns() alone never triggers a reconcile when its
                // readiness changes. Without this, determinePhase's
                // Pod-health-driven transitions only ever fire on the
                // periodic RequeueAfter, not reactively (found by an
                // envtest+manager integration test in Task 21: Running was
                // never reached within a generous timeout because nothing
                // re-triggered reconciliation after the Pod became Ready).
                Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(mapPodToInstance)).
                Complete(r)
}

func mapPodToInstance(_ context.Context, obj client.Object) []reconcile.Request {
        name, ok := obj.GetLabels()["kimo.io/instance"]
        if !ok {
                return nil
        }
        return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: obj.GetNamespace()}}}
}
```

Add `"sigs.k8s.io/controller-runtime/pkg/handler"` and `"sigs.k8s.io/controller-runtime/pkg/reconcile"` to the imports.

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestInstanceController -v
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement Instance Controller with container lifecycle state machine"
```

---

### Task 6: Backend Interface & Registry

**Files:**
- Create: `internal/integrations/backend.go`
- Create: `internal/integrations/backend_test.go`

This is the plugin seam every scoring-platform integration goes through — the piece that makes "integrate any backend easily" concrete rather than aspirational.

**Step 1: Write the failing test**

Create `internal/integrations/backend_test.go`:

```go
package integrations

import (
        "context"
        "encoding/json"
        "net/http"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

type stubBackend struct{ name string }

func (s *stubBackend) Name() string { return s.name }
func (s *stubBackend) Notify(context.Context, Event) error { return nil }
func (s *stubBackend) Authenticate(*http.Request) (Principal, error) { return Principal{}, nil }

func TestRegistry_RegisterAndNew(t *testing.T) {
        Register("stub-for-test", func(cfg json.RawMessage) (Backend, error) {
                return &stubBackend{name: "stub-for-test"}, nil
        })

        b, err := New("stub-for-test", nil)
        require.NoError(t, err)
        assert.Equal(t, "stub-for-test", b.Name())
}

func TestRegistry_UnknownBackend(t *testing.T) {
        _, err := New("does-not-exist", nil)
        assert.Error(t, err)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/integrations/ -run TestRegistry -v
```

**Step 3: Implement the interface and registry**

Create `internal/integrations/backend.go`:

```go
package integrations

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "time"
)

// EventType identifies a ChallengeInstance lifecycle transition.
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

// Event is dispatched to the active backend on every ChallengeInstance
// phase transition.
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
// the active backend's own auth scheme.
type Principal struct {
        Subject string
        Team    string
        Scopes  []string
}

// Backend is implemented by every scoring-platform integration. It is the
// single seam between KIMO's controllers/API and an external platform.
type Backend interface {
        Name() string
        Notify(ctx context.Context, event Event) error
        Authenticate(r *http.Request) (Principal, error)
}

// Factory constructs a Backend from its opaque, backend-specific config.
type Factory func(cfg json.RawMessage) (Backend, error)

var registry = map[string]Factory{}

// Register makes a backend available by name. Called from each adapter's
// init() — this is how a new integration becomes selectable without
// touching any controller or API code.
func Register(name string, factory Factory) {
        registry[name] = factory
}

// New constructs the named backend from config. Returns an error if no
// backend was registered under that name.
func New(name string, cfg json.RawMessage) (Backend, error) {
        factory, ok := registry[name]
        if !ok {
                return nil, fmt.Errorf("unknown scoring backend %q", name)
        }
        return factory(cfg)
}
```

**Step 4: Run tests**

```bash
go test ./internal/integrations/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add pluggable scoring backend interface and registry"
```

---

### Task 7: Generic Backend Adapter (default)

**Files:**
- Create: `internal/integrations/generic.go`
- Create: `internal/integrations/generic_test.go`

The `generic` backend is the zero-config default: HMAC-signed webhook fan-out plus a static Bearer API key, equivalent to what earlier revisions of this plan baked directly into the API server. Every deployment that doesn't need a specific platform integration uses this one unmodified.

**Step 1: Write the failing test**

Create `internal/integrations/generic_test.go`:

```go
package integrations

import (
        "context"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestGenericBackend_NotifySignsPayload(t *testing.T) {
        var gotSig, gotBody string
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                gotSig = r.Header.Get("X-KIMO-Signature")
                body := make([]byte, r.ContentLength)
                r.Body.Read(body)
                gotBody = string(body)
                w.WriteHeader(http.StatusOK)
        }))
        defer srv.Close()

        b, err := newGenericBackend(nil)
        require.NoError(t, err)
        gb := b.(*genericBackend)
        gb.RegisterWebhook(srv.URL, "shared-secret")

        err = gb.Notify(context.Background(), Event{Type: EventRunning, Instance: "web-sqli-team-1"})
        require.NoError(t, err)

        mac := hmac.New(sha256.New, []byte("shared-secret"))
        mac.Write([]byte(gotBody))
        assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), gotSig)
}

func TestGenericBackend_AuthenticateRejectsWrongKey(t *testing.T) {
        b, err := newGenericBackend(json.RawMessage(`{"apiKey":"secret-key"}`))
        require.NoError(t, err)

        req := httptest.NewRequest(http.MethodGet, "/", nil)
        req.Header.Set("Authorization", "Bearer wrong-key")
        _, err = b.Authenticate(req)
        assert.Error(t, err)
}

func TestGenericBackend_AuthenticateAcceptsValidKey(t *testing.T) {
        b, err := newGenericBackend(json.RawMessage(`{"apiKey":"secret-key"}`))
        require.NoError(t, err)

        req := httptest.NewRequest(http.MethodGet, "/", nil)
        req.Header.Set("Authorization", "Bearer secret-key")
        _, err = b.Authenticate(req)
        assert.NoError(t, err)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/integrations/ -run TestGenericBackend -v
```

**Step 3: Implement the generic backend**

Create `internal/integrations/generic.go`:

```go
package integrations

import (
        "bytes"
        "context"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
        "sync"
)

func init() {
        Register("generic", newGenericBackend)
}

type genericConfig struct {
        APIKey string `json:"apiKey"`
}

// genericBackend is the zero-config default: HMAC-signed webhook fan-out
// and a static Bearer API key.
type genericBackend struct {
        apiKey string
        mu     sync.RWMutex
        hooks  map[string]string // url -> hmac secret
}

func newGenericBackend(cfg json.RawMessage) (Backend, error) {
        var c genericConfig
        if len(cfg) > 0 {
                if err := json.Unmarshal(cfg, &c); err != nil {
                        return nil, fmt.Errorf("parsing generic backend config: %w", err)
                }
        }
        return &genericBackend{apiKey: c.APIKey, hooks: map[string]string{}}, nil
}

func (b *genericBackend) Name() string { return "generic" }

// RegisterWebhook implements integrations.WebhookRegistrar, letting the API
// server's /webhooks/configure endpoint stay backend-agnostic.
func (b *genericBackend) RegisterWebhook(url, secret string) error {
        b.mu.Lock()
        defer b.mu.Unlock()
        b.hooks[url] = secret
        return nil
}

func (b *genericBackend) Notify(ctx context.Context, event Event) error {
        b.mu.RLock()
        hooks := make(map[string]string, len(b.hooks))
        for u, s := range b.hooks {
                hooks[u] = s
        }
        b.mu.RUnlock()

        payload, err := json.Marshal(event)
        if err != nil {
                return err
        }

        var errs []error
        for url, secret := range hooks {
                if err := postSigned(ctx, url, secret, payload); err != nil {
                        errs = append(errs, err)
                }
        }
        return errors.Join(errs...)
}

func (b *genericBackend) Authenticate(r *http.Request) (Principal, error) {
        if r.Header.Get("Authorization") != "Bearer "+b.apiKey {
                return Principal{}, fmt.Errorf("unauthorized")
        }
        return Principal{Subject: "api-key", Scopes: []string{"admin"}}, nil
}

func postSigned(ctx context.Context, url, secret string, payload []byte) error {
        mac := hmac.New(sha256.New, []byte(secret))
        mac.Write(payload)
        sig := hex.EncodeToString(mac.Sum(nil))

        req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
        if err != nil {
                return err
        }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("X-KIMO-Signature", sig)

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 300 {
                return fmt.Errorf("webhook %s returned %d", url, resp.StatusCode)
        }
        return nil
}
```

Also add the `WebhookRegistrar` interface to `internal/integrations/backend.go` (Task 6's file):

```go
// WebhookRegistrar is an optional capability a Backend can implement to
// support runtime webhook registration via the REST API. Only the generic
// backend implements it today.
type WebhookRegistrar interface {
        RegisterWebhook(url, secret string) error
}
```

**Step 4: Run tests**

```bash
go test ./internal/integrations/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement generic HMAC-webhook scoring backend adapter"
```

---

### Task 8: CTFd Backend Adapter (worked example)

**Files:**
- Create: `internal/integrations/ctfd.go`
- Create: `internal/integrations/ctfd_test.go`

This adapter exists to prove out and document the "add a new backend" path from the design doc: a self-contained file, an `init()` registration, no controller or API changes required.

**Step 1: Write the failing test**

Create `internal/integrations/ctfd_test.go`:

```go
package integrations

import (
        "context"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestCTFdBackend_NotifyPostsTranslatedPayload(t *testing.T) {
        var gotType string
        webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                var body map[string]interface{}
                json.NewDecoder(r.Body).Decode(&body)
                gotType, _ = body["type"].(string)
                w.WriteHeader(http.StatusOK)
        }))
        defer webhook.Close()

        cfg, _ := json.Marshal(ctfdConfig{BaseURL: "https://ctfd.example.com", WebhookURL: webhook.URL, APIKey: "k"})
        b, err := newCTFdBackend(cfg)
        require.NoError(t, err)

        err = b.Notify(context.Background(), Event{Type: EventRunning, Challenge: "web-sqli", Team: "42"})
        require.NoError(t, err)
        assert.Equal(t, string(EventRunning), gotType)
}

func TestCTFdBackend_AuthenticateValidatesAgainstCTFd(t *testing.T) {
        ctfd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.Header.Get("Authorization") != "Token good-token" {
                        w.WriteHeader(http.StatusUnauthorized)
                        return
                }
                json.NewEncoder(w).Encode(map[string]interface{}{
                        "data": map[string]interface{}{"id": 1, "name": "team42", "team_id": 42},
                })
        }))
        defer ctfd.Close()

        cfg, _ := json.Marshal(ctfdConfig{BaseURL: ctfd.URL})
        b, err := newCTFdBackend(cfg)
        require.NoError(t, err)

        req := httptest.NewRequest(http.MethodGet, "/", nil)
        req.Header.Set("Authorization", "Token good-token")
        principal, err := b.Authenticate(req)
        require.NoError(t, err)
        assert.Equal(t, "team42", principal.Subject)
        assert.Equal(t, "42", principal.Team)

        req.Header.Set("Authorization", "Token bad-token")
        _, err = b.Authenticate(req)
        assert.Error(t, err)
}

func TestCTFdBackend_RequiresBaseURL(t *testing.T) {
        _, err := newCTFdBackend(json.RawMessage(`{}`))
        assert.Error(t, err)
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/integrations/ -run TestCTFdBackend -v
```

**Step 3: Implement the CTFd adapter**

Create `internal/integrations/ctfd.go`:

```go
package integrations

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "net/http"
)

func init() {
        Register("ctfd", newCTFdBackend)
}

type ctfdConfig struct {
        BaseURL    string `json:"baseUrl"`    // CTFd instance, used for Authenticate
        WebhookURL string `json:"webhookUrl"` // where lifecycle events are POSTed; optional
        APIKey     string `json:"apiKey"`     // KIMO -> CTFd calls
}

type ctfdBackend struct {
        cfg    ctfdConfig
        client *http.Client
}

func newCTFdBackend(cfg json.RawMessage) (Backend, error) {
        var c ctfdConfig
        if err := json.Unmarshal(cfg, &c); err != nil {
                return nil, fmt.Errorf("parsing ctfd backend config: %w", err)
        }
        if c.BaseURL == "" {
                return nil, fmt.Errorf("ctfd backend requires baseUrl")
        }
        return &ctfdBackend{cfg: c, client: http.DefaultClient}, nil
}

func (b *ctfdBackend) Name() string { return "ctfd" }

type ctfdEventPayload struct {
        Type      string `json:"type"`
        Challenge string `json:"challenge_id"`
        Team      string `json:"team_id"`
        URL       string `json:"instance_url"`
}

func (b *ctfdBackend) Notify(ctx context.Context, event Event) error {
        if b.cfg.WebhookURL == "" {
                return nil // no CTFd webhook configured — Notify is a no-op
        }
        body, err := json.Marshal(ctfdEventPayload{
                Type:      string(event.Type),
                Challenge: event.Challenge,
                Team:      event.Team,
                URL:       event.Endpoint,
        })
        if err != nil {
                return err
        }
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.WebhookURL, bytes.NewReader(body))
        if err != nil {
                return err
        }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Token "+b.cfg.APIKey)

        resp, err := b.client.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 300 {
                return fmt.Errorf("ctfd webhook returned %d", resp.StatusCode)
        }
        return nil
}

// Authenticate validates the caller's CTFd API token by calling CTFd's own
// /api/v1/users/me — KIMO never stores or issues its own credentials for
// this backend, CTFd remains the source of truth.
func (b *ctfdBackend) Authenticate(r *http.Request) (Principal, error) {
        token := r.Header.Get("Authorization")
        if token == "" {
                return Principal{}, fmt.Errorf("missing authorization header")
        }

        req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, b.cfg.BaseURL+"/api/v1/users/me", nil)
        if err != nil {
                return Principal{}, err
        }
        req.Header.Set("Authorization", token)

        resp, err := b.client.Do(req)
        if err != nil {
                return Principal{}, err
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return Principal{}, fmt.Errorf("ctfd rejected credentials: %d", resp.StatusCode)
        }

        var body struct {
                Data struct {
                        Name   string `json:"name"`
                        TeamID int    `json:"team_id"`
                } `json:"data"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
                return Principal{}, err
        }
        return Principal{Subject: body.Data.Name, Team: fmt.Sprintf("%d", body.Data.TeamID)}, nil
}
```

**Step 4: Run tests**

```bash
go test ./internal/integrations/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add CTFd scoring backend adapter"
```

---

### Task 9: Network Controller

**Files:**
- Modify: `internal/controller/networkfence_controller.go`
- Create: `internal/controller/networkfence_controller_test.go`

**Step 1: Write the failing test**

Create `internal/controller/networkfence_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        networkingv1 "k8s.io/api/networking/v1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "sigs.k8s.io/controller-runtime/pkg/client/fake"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestNetworkController_CreatesNetworkPolicy(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))
        require.NoError(t, networkingv1.AddToScheme(scheme))

        fence := &kimov1alpha1.NetworkFence{
                ObjectMeta: metav1.ObjectMeta{Name: "web-sqli-team-1", Namespace: "default"},
                Spec: kimov1alpha1.NetworkFenceSpec{
                        InstanceRef: "web-sqli-team-1",
                        AllowRules:  []kimov1alpha1.AllowRule{{Port: 8080}},
                        DenyRules:   []kimov1alpha1.DenyRule{{To: "kimo-system"}},
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(fence).
                WithStatusSubresource(fence).
                Build()

        r := &NetworkFenceReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        var np networkingv1.NetworkPolicy
        err = client.Get(context.Background(),
                types.NamespacedName{Name: "kimo-web-sqli-team-1", Namespace: "default"}, &np)
        require.NoError(t, err)
        assert.Equal(t, "kimo.io/instance", np.Spec.PodSelector.MatchLabels["kimo.io/instance"])

        var updated kimov1alpha1.NetworkFence
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
        assert.True(t, updated.Status.Applied)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/controller/ -run TestNetworkController -v
```

**Step 3: Implement the Network Controller**

Edit `internal/controller/networkfence_controller.go`:

```go
package controller

import (
        "context"
        "fmt"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        corev1 "k8s.io/api/core/v1"
        networkingv1 "k8s.io/api/networking/v1"
        "k8s.io/apimachinery/pkg/api/errors"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "k8s.io/apimachinery/pkg/util/intstr"
        ctrl "sigs.k8s.io/controller-runtime"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
        "sigs.k8s.io/controller-runtime/pkg/log"
)

type NetworkFenceReconciler struct {
        client.Client
        Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=networkfences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=networkfences/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

func (r *NetworkFenceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
        logger := log.FromContext(ctx)

        var fence kimov1alpha1.NetworkFence
        if err := r.Get(ctx, req.NamespacedName, &fence); err != nil {
                if errors.IsNotFound(err) {
                        return ctrl.Result{}, nil
                }
                return ctrl.Result{}, err
        }

        np := r.buildNetworkPolicy(&fence)
        if err := controllerutil.SetControllerReference(&fence, np, r.Scheme); err != nil {
                return ctrl.Result{}, err
        }

        var existing networkingv1.NetworkPolicy
        if err := r.Get(ctx, types.NamespacedName{Name: np.Name, Namespace: np.Namespace}, &existing); err != nil {
                if errors.IsNotFound(err) {
                        if err := r.Create(ctx, np); err != nil {
                                return ctrl.Result{}, fmt.Errorf("creating network policy: %w", err)
                        }
                } else {
                        return ctrl.Result{}, err
                }
        } else {
                existing.Spec = np.Spec
                if err := r.Update(ctx, &existing); err != nil {
                        return ctrl.Result{}, fmt.Errorf("updating network policy: %w", err)
                }
        }

        fence.Status.Applied = true
        fence.Status.Message = ""
        if err := r.Status().Update(ctx, &fence); err != nil {
                return ctrl.Result{}, err
        }

        logger.Info("network fence applied", "name", fence.Name)
        return ctrl.Result{}, nil
}

func (r *NetworkFenceReconciler) buildNetworkPolicy(fence *kimov1alpha1.NetworkFence) *networkingv1.NetworkPolicy {
        protocol := corev1.ProtocolTCP

        var ingressPorts []networkingv1.NetworkPolicyPort
        for _, allow := range fence.Spec.AllowRules {
                if allow.Port > 0 {
                        port := intstr.FromInt32(allow.Port)
                        ingressPorts = append(ingressPorts, networkingv1.NetworkPolicyPort{Port: &port, Protocol: &protocol})
                }
        }

        var ingressFrom []networkingv1.NetworkPolicyPeer
        for _, allow := range fence.Spec.AllowRules {
                if allow.CIDR != "" {
                        ingressFrom = append(ingressFrom, networkingv1.NetworkPolicyPeer{IPBlock: &networkingv1.IPBlock{CIDR: allow.CIDR}})
                }
        }

        ingress := []networkingv1.NetworkPolicyIngressRule{{Ports: ingressPorts, From: ingressFrom}}

        var egressRules []networkingv1.NetworkPolicyEgressRule
        if fence.Spec.AllowEgress {
                egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{})
        }

        policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
        if len(egressRules) > 0 || len(fence.Spec.DenyRules) > 0 {
                policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
        }

        return &networkingv1.NetworkPolicy{
                ObjectMeta: metav1.ObjectMeta{Name: "kimo-" + fence.Spec.InstanceRef, Namespace: fence.Namespace},
                Spec: networkingv1.NetworkPolicySpec{
                        PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"kimo.io/instance": fence.Spec.InstanceRef}},
                        Ingress:     ingress,
                        Egress:      egressRules,
                        PolicyTypes: policyTypes,
                },
        }
}

func (r *NetworkFenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
                For(&kimov1alpha1.NetworkFence{}).
                Owns(&networkingv1.NetworkPolicy{}).
                Complete(r)
}
```

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestNetworkController -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement Network Controller with NetworkPolicy creation"
```

---

### Task 10: Lifecycle Controller (TTL + Expiry)

**Files:**
- Create: `internal/controller/lifecycle_controller.go` (kubebuilder won't scaffold this since it doesn't own a CRD)
- Create: `internal/controller/lifecycle_controller_test.go`

Drives the tail of the state machine: `Running/Unhealthy -> Expiring -> Expired`, notifying the backend at each step so a scoring platform can, e.g., warn players before teardown.

**Step 1: Write the failing test**

Create `internal/controller/lifecycle_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/hermannchristopher/kimo/internal/integrations"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "sigs.k8s.io/controller-runtime/pkg/client/fake"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestLifecycleController_EntersExpiringWithinGraceWindow(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        soon := metav1.NewTime(time.Now().Add(30 * time.Second)) // inside the 60s grace window
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "soon-inst", Namespace: "default"},
                Spec:       kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "test", Team: "team-1"},
                Status:     kimov1alpha1.ChallengeInstanceStatus{Phase: kimov1alpha1.InstancePhaseRunning, ExpiresAt: &soon},
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(instance).WithStatusSubresource(instance).Build()

        backend := &recordingBackend{}
        r := &LifecycleReconciler{Client: client, Scheme: scheme, Backend: backend}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "soon-inst", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "soon-inst", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseExpiring, updated.Status.Phase)
        require.Len(t, backend.events, 1)
        assert.Equal(t, integrations.EventExpiring, backend.events[0].Type)
}

func TestLifecycleController_ExpiresInstance(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        pastTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "expired-inst", Namespace: "default"},
                Spec:       kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "test", Team: "team-1"},
                Status:     kimov1alpha1.ChallengeInstanceStatus{Phase: kimov1alpha1.InstancePhaseExpiring, ExpiresAt: &pastTime},
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(instance).WithStatusSubresource(instance).Build()

        backend := &recordingBackend{}
        r := &LifecycleReconciler{Client: client, Scheme: scheme, Backend: backend}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "expired-inst", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "expired-inst", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseExpired, updated.Status.Phase)
        require.Len(t, backend.events, 1)
        assert.Equal(t, integrations.EventExpired, backend.events[0].Type)
}

func TestLifecycleController_SkipsInstanceWithPlentyOfTimeLeft(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        futureTime := metav1.NewTime(time.Now().Add(30 * time.Minute))
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "active-inst", Namespace: "default"},
                Spec:       kimov1alpha1.ChallengeInstanceSpec{TemplateRef: "test", Team: "team-1"},
                Status:     kimov1alpha1.ChallengeInstanceStatus{Phase: kimov1alpha1.InstancePhaseRunning, ExpiresAt: &futureTime},
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(instance).WithStatusSubresource(instance).Build()

        backend := &recordingBackend{}
        r := &LifecycleReconciler{Client: client, Scheme: scheme, Backend: backend}
        result, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "active-inst", Namespace: "default"},
        })
        require.NoError(t, err)
        assert.NotZero(t, result.RequeueAfter)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "active-inst", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseRunning, updated.Status.Phase)
        assert.Empty(t, backend.events)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/controller/ -run TestLifecycleController -v
```

**Step 3: Implement the Lifecycle Controller**

Create `internal/controller/lifecycle_controller.go`:

```go
package controller

import (
        "context"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/hermannchristopher/kimo/internal/integrations"
        "k8s.io/apimachinery/pkg/api/errors"
        "k8s.io/apimachinery/pkg/runtime"
        ctrl "sigs.k8s.io/controller-runtime"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/log"
)

const expiringGraceWindow = 60 * time.Second

type LifecycleReconciler struct {
        client.Client
        Scheme  *runtime.Scheme
        Backend integrations.Backend
}

func (r *LifecycleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
        logger := log.FromContext(ctx)

        var instance kimov1alpha1.ChallengeInstance
        if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
                if errors.IsNotFound(err) {
                        return ctrl.Result{}, nil
                }
                return ctrl.Result{}, err
        }

        if terminal(instance.Status.Phase) || instance.Status.ExpiresAt == nil {
                return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }

        now := time.Now()
        remaining := instance.Status.ExpiresAt.Time.Sub(now)

        switch {
        case remaining <= 0:
                logger.Info("expiring instance", "name", instance.Name)
                return r.notifyAndSetPhase(ctx, &instance, kimov1alpha1.InstancePhaseExpired, "TTL expired", integrations.EventExpired, 60*time.Second)
        case remaining <= expiringGraceWindow && instance.Status.Phase != kimov1alpha1.InstancePhaseExpiring:
                return r.notifyAndSetPhase(ctx, &instance, kimov1alpha1.InstancePhaseExpiring, "entering expiry grace window", integrations.EventExpiring, remaining)
        default:
                return ctrl.Result{RequeueAfter: remaining - expiringGraceWindow}, nil
        }
}

func (r *LifecycleReconciler) notifyAndSetPhase(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, phase kimov1alpha1.InstancePhase, reason string, evt integrations.EventType, requeueAfter time.Duration) (ctrl.Result, error) {
        instance.Status.Phase = phase
        instance.Status.Reason = reason
        if err := r.Status().Update(ctx, instance); err != nil {
                return ctrl.Result{}, err
        }
        if r.Backend != nil {
                _ = r.Backend.Notify(ctx, integrations.Event{
                        Type:      evt,
                        Instance:  instance.Name,
                        Challenge: instance.Spec.TemplateRef,
                        Team:      instance.Spec.Team,
                        Player:    instance.Spec.Player,
                        Endpoint:  instance.Status.Endpoint,
                        Reason:    reason,
                })
        }
        return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *LifecycleReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
                For(&kimov1alpha1.ChallengeInstance{}).
                Named("lifecycle").
                Complete(r)
}
```

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestLifecycleController -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement Lifecycle Controller with Expiring/Expired phases"
```

---

### Task 11: Set Controller

**Files:**
- Modify: `internal/controller/challengeset_controller.go`
- Create: `internal/controller/challengeset_controller_test.go`

**Step 1: Write the failing test**

```go
package controller

// TestSetController_ActivatesOnSchedule tests that a ChallengeSet becomes active
// when current time is within its schedule window.
func TestSetController_ActivatesOnSchedule(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        now := time.Now()
        set := &kimov1alpha1.ChallengeSet{
                ObjectMeta: metav1.ObjectMeta{Name: "round-1", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeSetSpec{
                        Challenges: []string{"web-sqli", "web-xss"},
                        Schedule: &kimov1alpha1.ScheduleSpec{
                                StartAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
                                EndAt:   now.Add(1 * time.Hour).Format(time.RFC3339),
                        },
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(set).
                WithStatusSubresource(set).
                Build()

        r := &ChallengeSetReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "round-1", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeSet
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "round-1", Namespace: "default"}, &updated))
        assert.True(t, updated.Status.Active)
}
```

**Step 2: Run test to verify failure, then implement**

Implement `ChallengeSetReconciler` in `internal/controller/challengeset_controller.go` — parse schedule, check if `now` is within window, set `status.active`. Requeue at `endAt` to deactivate.

**Step 3: Run tests, commit**

```bash
go test ./internal/controller/ -run TestSetController -v
git add -A
git commit -m "feat: implement Set Controller with schedule management"
```

---

### Task 12: REST API Server — Core

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/handlers.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/server_test.go`

Auth is now delegated to the active `integrations.Backend` — the server no longer owns an API key directly.

**Step 1: Add chi dependency**

```bash
go get github.com/go-chi/chi/v5
```

**Step 2: Write the failing test**

Create `internal/api/server_test.go`:

```go
package api

import (
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/hermannchristopher/kimo/internal/integrations"
        "github.com/stretchr/testify/assert"
)

type stubAuthBackend struct{ apiKey string }

func (s *stubAuthBackend) Name() string { return "stub" }
func (s *stubAuthBackend) Notify(_ context.Context, _ integrations.Event) error { return nil }
func (s *stubAuthBackend) Authenticate(r *http.Request) (integrations.Principal, error) {
        if r.Header.Get("Authorization") != "Bearer "+s.apiKey {
                return integrations.Principal{}, fmt.Errorf("unauthorized")
        }
        return integrations.Principal{Subject: "test"}, nil
}

func TestHealthEndpoint(t *testing.T) {
        srv := NewServer(nil, &stubAuthBackend{apiKey: "test-key"})
        req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
        srv := NewServer(nil, &stubAuthBackend{apiKey: "secret-key"})
        req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_AcceptsValidKey(t *testing.T) {
        srv := NewServer(nil, &stubAuthBackend{apiKey: "secret-key"})
        req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
        req.Header.Set("Authorization", "Bearer secret-key")
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}
```

**Step 3: Implement server, middleware, handlers**

Create `internal/api/server.go`:

```go
package api

import (
        "context"
        "net/http"

        "github.com/go-chi/chi/v5"
        "github.com/go-chi/chi/v5/middleware"
        "github.com/hermannchristopher/kimo/internal/integrations"
        "sigs.k8s.io/controller-runtime/pkg/client"
)

type Server struct {
        client  client.Client
        backend integrations.Backend
        router  chi.Router
}

func NewServer(c client.Client, backend integrations.Backend) *Server {
        s := &Server{client: c, backend: backend}
        s.setupRoutes()
        return s
}

func (s *Server) Router() chi.Router { return s.router }

func (s *Server) setupRoutes() {
        r := chi.NewRouter()
        r.Use(middleware.Logger)
        r.Use(middleware.Recoverer)

        r.Get("/api/v1/health", s.handleHealth)

        r.Group(func(r chi.Router) {
                r.Use(s.authMiddleware)
                r.Get("/api/v1/templates", s.handleListTemplates)
                r.Get("/api/v1/templates/{name}", s.handleGetTemplate)
                r.Post("/api/v1/instances", s.handleCreateInstance)
                r.Get("/api/v1/instances", s.handleListInstances)
                r.Get("/api/v1/instances/{name}", s.handleGetInstance)
                r.Delete("/api/v1/instances/{name}", s.handleDeleteInstance)
                r.Patch("/api/v1/instances/{name}/extend", s.handleExtendInstance)
                r.Get("/api/v1/pow/challenge", s.handlePoWChallenge)
                r.Post("/api/v1/webhooks/configure", s.handleConfigureWebhook)
        })

        s.router = r
}

type principalContextKey struct{}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                principal, err := s.backend.Authenticate(r)
                if err != nil {
                        http.Error(w, "unauthorized", http.StatusUnauthorized)
                        return
                }
                ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
                next.ServeHTTP(w, r.WithContext(ctx))
        })
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"status":"ok"}`))
}

// handleConfigureWebhook only works when the active backend supports runtime
// webhook registration (today: the generic backend). Other backends return
// 501 — their integration is configured via Helm values instead.
func (s *Server) handleConfigureWebhook(w http.ResponseWriter, r *http.Request) {
        registrar, ok := s.backend.(integrations.WebhookRegistrar)
        if !ok {
                http.Error(w, "active scoring backend does not support webhook registration", http.StatusNotImplemented)
                return
        }
        var body struct {
                URL    string `json:"url"`
                Secret string `json:"secret"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                http.Error(w, "invalid body", http.StatusBadRequest)
                return
        }
        if err := registrar.RegisterWebhook(body.URL, body.Secret); err != nil {
                http.Error(w, err.Error(), http.StatusInternalServerError)
                return
        }
        w.WriteHeader(http.StatusOK)
}
```

Create `internal/api/handlers.go` with CRUD handler stubs that use the K8s client to list/get/create/delete ChallengeInstance and ChallengeTemplate CRs.

**Step 4: Run tests**

```bash
go test ./internal/api/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement REST API server delegating auth to the scoring backend"
```

---

### Task 13: REST API — Instance CRUD Handlers

**Files:**
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/server_test.go`

**Step 1: Write failing tests for instance creation and listing**

Add tests that POST to `/api/v1/instances` with a JSON body `{"template":"web-sqli","team":"team-1"}` and verify a ChallengeInstance CR is created. Add tests for GET listing with query params.

**Step 2: Implement handlers**

Each handler interacts with the K8s client:
- `handleCreateInstance`: validate body, check template exists, check PoW if required, create ChallengeInstance CR
- `handleListInstances`: list CRs with label selectors for team/challenge/status filters
- `handleGetInstance`: get CR, return status with endpoint
- `handleDeleteInstance`: delete CR (cascade deletes owned resources)
- `handleExtendInstance`: update `spec.ttlOverride`

Lifecycle notifications (`instance.creating`, `instance.running`, ...) are dispatched by the Instance and Lifecycle controllers as phase transitions happen (Tasks 5 and 10) — these handlers only mutate the CR, they never call `Backend.Notify` directly.

**Step 3: Run tests, commit**

```bash
go test ./internal/api/ -v
git add -A
git commit -m "feat: implement instance CRUD API handlers"
```

---

### Task 14: Proof of Work System

**Files:**
- Create: `internal/api/pow.go`
- Create: `internal/api/pow_test.go`

**Step 1: Write the failing test**

Create `internal/api/pow_test.go`:

```go
package api

import (
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestPoW_GenerateAndVerify(t *testing.T) {
        puzzle := GeneratePoWPuzzle(16) // 16 leading zero bits = easy
        require.NotEmpty(t, puzzle.Challenge)
        require.Equal(t, 16, puzzle.Difficulty)

        nonce, found := SolvePoW(puzzle.Challenge, puzzle.Difficulty)
        require.True(t, found)

        assert.True(t, VerifyPoW(puzzle.Challenge, nonce, puzzle.Difficulty))
}

func TestPoW_RejectsWrongNonce(t *testing.T) {
        puzzle := GeneratePoWPuzzle(16)
        assert.False(t, VerifyPoW(puzzle.Challenge, 0, puzzle.Difficulty))
}

func TestPoW_LeadingZeroBits(t *testing.T) {
        challenge := "test-challenge"
        nonce := uint64(12345)
        hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", challenge, nonce)))
        hexHash := hex.EncodeToString(hash[:])
        _ = hexHash
        assert.GreaterOrEqual(t, countLeadingZeroBits(hash[:]), 0)
}
```

**Step 2: Implement PoW**

Create `internal/api/pow.go`:

```go
package api

import (
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "time"
)

type PoWPuzzle struct {
        Challenge  string    `json:"challenge"`
        Difficulty int       `json:"difficulty"`
        ExpiresAt  time.Time `json:"expiresAt"`
}

func GeneratePoWPuzzle(difficulty int) PoWPuzzle {
        b := make([]byte, 32)
        rand.Read(b)
        return PoWPuzzle{
                Challenge:  hex.EncodeToString(b),
                Difficulty: difficulty,
                ExpiresAt:  time.Now().Add(5 * time.Minute),
        }
}

func VerifyPoW(challenge string, nonce uint64, difficulty int) bool {
        data := fmt.Sprintf("%s:%d", challenge, nonce)
        hash := sha256.Sum256([]byte(data))
        return countLeadingZeroBits(hash[:]) >= difficulty
}

func countLeadingZeroBits(hash []byte) int {
        count := 0
        for _, b := range hash {
                if b == 0 {
                        count += 8
                        continue
                }
                for i := 7; i >= 0; i-- {
                        if b&(1<<uint(i)) == 0 {
                                count++
                        } else {
                                return count
                        }
                }
        }
        return count
}

// SolvePoW is a helper for testing — brute-force solver.
func SolvePoW(challenge string, difficulty int) (uint64, bool) {
        for nonce := uint64(0); nonce < 1<<30; nonce++ {
                if VerifyPoW(challenge, nonce, difficulty) {
                        return nonce, true
                }
        }
        return 0, false
}
```

**Step 3: Wire PoW into the API handler**

Update `handlePoWChallenge` and `handleCreateInstance` to generate/verify puzzles.

**Step 4: Run tests, commit**

```bash
go test ./internal/api/ -v
git add -A
git commit -m "feat: implement Proof of Work system"
```

---

### Task 15: Wire Up Manager Entrypoint

**Files:**
- Modify: `cmd/manager/main.go`

**Step 1: Resolve the scoring backend from config and register all controllers**

Edit `cmd/manager/main.go` to:
- Read `KIMO_BACKEND` (defaults to `generic`) and `KIMO_BACKEND_CONFIG` (JSON) from the environment
- Construct the backend via `integrations.New`
- Register all 5 controllers with the manager, injecting the resolved `Backend` into `ChallengeInstanceReconciler` and `LifecycleReconciler`
- Start the REST API server in a goroutine, passing it the same `Backend`

```go
// In main():
backendName := os.Getenv("KIMO_BACKEND")
if backendName == "" {
        backendName = "generic"
}
backend, err := integrations.New(backendName, json.RawMessage(os.Getenv("KIMO_BACKEND_CONFIG")))
if err != nil {
        setupLog.Error(err, "unable to initialize scoring backend", "backend", backendName)
        os.Exit(1)
}

if err = (&controller.ChallengeInstanceReconciler{
        Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Backend: backend,
}).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", "ChallengeInstance")
        os.Exit(1)
}
if err = (&controller.LifecycleReconciler{
        Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Backend: backend,
}).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", "Lifecycle")
        os.Exit(1)
}
// ... Template, Network, Set controllers registered the same way (no Backend needed)

apiServer := api.NewServer(mgr.GetClient(), backend)
go func() {
        if err := http.ListenAndServe(":8080", apiServer.Router()); err != nil {
                setupLog.Error(err, "API server failed")
                os.Exit(1)
        }
}()
```

**Step 2: Verify build**

```bash
make build
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: wire up all controllers, scoring backend, and API server in manager"
```

---

### Task 16: Discord Bot — Core Setup

**Files:**
- Create: `cmd/bot/main.go`
- Create: `internal/bot/bot.go`
- Create: `internal/bot/client.go`

**Step 1: Add discordgo dependency**

```bash
go get github.com/bwmarrin/discordgo
```

**Step 2: Implement KIMO API client**

Create `internal/bot/client.go` — a simple HTTP client wrapper for the KIMO REST API:

```go
package bot

import (
        "encoding/json"
        "fmt"
        "net/http"
)

type KIMOClient struct {
        baseURL string
        apiKey  string
        http    *http.Client
}

func NewKIMOClient(baseURL, apiKey string) *KIMOClient {
        return &KIMOClient{
                baseURL: baseURL,
                apiKey:  apiKey,
                http:    &http.Client{},
        }
}

func (c *KIMOClient) do(method, path string, body interface{}) (*http.Response, error) {
        // Build request, set auth header, execute
}

func (c *KIMOClient) ListTemplates() ([]Template, error) { ... }
func (c *KIMOClient) CreateInstance(template, team string) (*Instance, error) { ... }
func (c *KIMOClient) ListInstances(team, challenge string) ([]Instance, error) { ... }
func (c *KIMOClient) DeleteInstance(name string) error { ... }
func (c *KIMOClient) ExtendInstance(name, duration string) error { ... }
```

**Step 3: Implement bot setup**

Create `internal/bot/bot.go`:

```go
package bot

import (
        "log"

        "github.com/bwmarrin/discordgo"
)

type Bot struct {
        session *discordgo.Session
        kimo    *KIMOClient
        config  Config
}

type Config struct {
        Token      string
        KIMOUrl    string
        KIMOApiKey string
        AdminRole  string
        OrgRole    string
}

func New(cfg Config) (*Bot, error) {
        session, err := discordgo.New("Bot " + cfg.Token)
        if err != nil {
                return nil, err
        }
        b := &Bot{
                session: session,
                kimo:    NewKIMOClient(cfg.KIMOUrl, cfg.KIMOApiKey),
                config:  cfg,
        }
        b.registerCommands()
        return b, nil
}

func (b *Bot) Start() error {
        return b.session.Open()
}

func (b *Bot) Stop() error {
        return b.session.Close()
}
```

**Step 4: Create entrypoint**

Create `cmd/bot/main.go`:

```go
package main

import (
        "log"
        "os"
        "os/signal"

        "github.com/hermannchristopher/kimo/internal/bot"
)

func main() {
        cfg := bot.Config{
                Token:      os.Getenv("DISCORD_TOKEN"),
                KIMOUrl:    os.Getenv("KIMO_API_URL"),
                KIMOApiKey: os.Getenv("KIMO_API_KEY"),
                AdminRole:  os.Getenv("DISCORD_ADMIN_ROLE"),
                OrgRole:    os.Getenv("DISCORD_ORG_ROLE"),
        }

        b, err := bot.New(cfg)
        if err != nil {
                log.Fatal(err)
        }
        if err := b.Start(); err != nil {
                log.Fatal(err)
        }
        defer b.Stop()

        stop := make(chan os.Signal, 1)
        signal.Notify(stop, os.Interrupt)
        <-stop
}
```

**Step 5: Verify build**

```bash
go build ./cmd/bot/
```

**Step 6: Commit**

```bash
git add -A
git commit -m "feat: add Discord bot core setup and KIMO API client"
```

---

### Task 17: Discord Bot — Slash Commands

**Files:**
- Create: `internal/bot/commands.go`
- Create: `internal/bot/commands_test.go`

**Step 1: Register slash commands**

Implement command registration and handlers in `internal/bot/commands.go`:

- `/challenges list` → calls `kimo.ListTemplates()`, formats as embed
- `/challenges status <name>` → calls `kimo.GetTemplate(name)`, shows instance counts
- `/instance create <template> <team>` → calls `kimo.CreateInstance()`
- `/instance destroy <name>` → calls `kimo.DeleteInstance()`
- `/instance extend <name> <duration>` → calls `kimo.ExtendInstance()`
- `/instance list` → calls `kimo.ListInstances()` with filters
- `/stats` → calls multiple endpoints, formats dashboard embed

**Step 2: Implement RBAC checks**

Create `internal/bot/rbac.go`:

```go
package bot

import "github.com/bwmarrin/discordgo"

func (b *Bot) hasRole(member *discordgo.Member, roleName string) bool {
        for _, roleID := range member.Roles {
                if roleID == roleName {
                        return true
                }
        }
        return false
}

func (b *Bot) isAdmin(member *discordgo.Member) bool {
        return b.hasRole(member, b.config.AdminRole)
}

func (b *Bot) isOrganizer(member *discordgo.Member) bool {
        return b.hasRole(member, b.config.OrgRole) || b.isAdmin(member)
}
```

**Step 3: Write unit tests for command response formatting**

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: implement Discord bot slash commands and RBAC"
```

---

### Task 18: Discord Bot — Event Monitor

**Files:**
- Create: `internal/bot/monitor.go`
- Create: `internal/bot/webhook_handler.go`

**Step 1: Implement webhook receiver**

The bot runs a small HTTP server that receives events from the `generic` scoring backend's webhook fan-out and posts to Discord. (When a different backend, e.g. `ctfd`, is active, `/monitor` documents that event streaming instead comes from that platform's own tooling.)

```go
package bot

import (
        "encoding/json"
        "net/http"

        "github.com/bwmarrin/discordgo"
)

type Monitor struct {
        session   *discordgo.Session
        channelID string
        active    bool
}

func (m *Monitor) HandleWebhook(w http.ResponseWriter, r *http.Request) {
        var event WebhookEvent
        if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
                http.Error(w, "bad request", http.StatusBadRequest)
                return
        }

        embed := m.formatEvent(event)
        m.session.ChannelMessageSendEmbed(m.channelID, embed)
        w.WriteHeader(http.StatusOK)
}

func (m *Monitor) formatEvent(event WebhookEvent) *discordgo.MessageEmbed {
        color := 0x00FF00 // green for running
        switch event.Event {
        case "instance.failed":
                color = 0xFF0000
        case "instance.unhealthy":
                color = 0xFFA500
        case "instance.expiring", "instance.expired":
                color = 0xFFA500
        case "instance.creating":
                color = 0xFFFF00
        }

        return &discordgo.MessageEmbed{
                Title:       event.Instance,
                Description: event.Event,
                Color:       color,
                Fields: []*discordgo.MessageEmbedField{
                        {Name: "Challenge", Value: event.Challenge, Inline: true},
                        {Name: "Team", Value: event.Team, Inline: true},
                        {Name: "Endpoint", Value: event.Endpoint, Inline: false},
                },
        }
}
```

**Step 2: Wire `/monitor start` and `/monitor stop` commands**

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: implement Discord bot event monitoring via generic backend webhooks"
```

---

### Task 19: Helm Chart

**Files:**
- Create: `helm/kimo/Chart.yaml`
- Create: `helm/kimo/values.yaml`
- Create: `helm/kimo/templates/deployment.yaml`
- Create: `helm/kimo/templates/service.yaml`
- Create: `helm/kimo/templates/rbac.yaml`
- Create: `helm/kimo/templates/bot-deployment.yaml`
- Create: `helm/kimo/templates/configmap.yaml`
- Create: `helm/kimo/templates/secret.yaml`

**Step 1: Create Chart.yaml**

```yaml
apiVersion: v2
name: kimo
description: Kubernetes Instance Manager Operator for CTF challenges
version: 0.1.0
appVersion: "0.1.0"
```

**Step 2: Create values.yaml**

`integration.backend` selects the scoring backend adapter; `integration.config` is passed through opaque to that adapter.

```yaml
operator:
  image: ghcr.io/hermannchristopher/kimo:latest
  replicas: 1
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits: { cpu: 500m, memory: 512Mi }

api:
  port: 8080

integration:
  backend: generic       # generic | ctfd | rctf | <custom, once registered>
  config: {}              # backend-specific; e.g. for generic: { apiKey: "..." }
                           #                    for ctfd:    { baseUrl: "...", webhookUrl: "...", apiKey: "..." }

bot:
  enabled: false
  image: ghcr.io/hermannchristopher/kimo-bot:latest
  discordToken: ""
  kimoApiUrl: "http://kimo-api:8080"
  adminRole: ""
  orgRole: ""

ctfDomain: ctf.example.com

global:
  maxInstances: 1000
```

**Step 3: Create templates**

Write standard Kubernetes Deployment, Service, ServiceAccount, ClusterRole, ClusterRoleBinding templates for the operator and bot. The operator Deployment sets `KIMO_BACKEND` and `KIMO_BACKEND_CONFIG` env vars from `.Values.integration` (config as a Secret when it contains credentials). Include CRD installation.

**Step 4: Lint chart**

```bash
helm lint helm/kimo/
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add Helm chart with pluggable scoring backend configuration"
```

---

### Task 20: Dockerfile

**Files:**
- Modify: `Dockerfile`

**Step 1: Write multi-stage Dockerfile**

```dockerfile
FROM golang:1.22 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o manager ./cmd/manager/
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot/

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/bot .
USER 65532:65532
ENTRYPOINT ["/manager"]
```

**Step 2: Build**

```bash
docker build -t kimo:dev .
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add multi-stage Dockerfile for operator and bot"
```

---

### Task 21: Integration Tests (envtest)

**Files:**
- Create: `test/integration/suite_test.go`
- Create: `test/integration/template_test.go`
- Create: `test/integration/instance_test.go`

**Step 1: Set up envtest suite**

```go
package integration

import (
        "testing"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "k8s.io/client-go/kubernetes/scheme"
        "sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testEnv *envtest.Environment

func TestMain(m *testing.M) {
        testEnv = &envtest.Environment{
                CRDDirectoryPaths: []string{"../../config/crd/bases"},
        }
        cfg, err := testEnv.Start()
        // ... setup client, run tests, teardown
}
```

**Step 2: Write integration tests**

- Create a ChallengeTemplate → verify status becomes ready
- Create a ChallengeInstance → verify Deployment and Service created, phase reaches `Creating`
- Mark the owned Pod ready → verify phase reaches `Running`
- Wait for TTL → verify instance moves through `Expiring` into `Expired`
- Use a `recordingBackend` test double (or reuse the one from Task 5) wired into the envtest manager to assert `Notify` fires for each transition

**Step 3: Run**

```bash
go test ./test/integration/ -v -timeout 120s
```

**Step 4: Commit**

```bash
git add -A
git commit -m "test: add envtest integration tests"
```

---

### Task 22: E2E Tests (kind)

**Files:**
- Create: `test/e2e/e2e_test.go`
- Create: `test/e2e/setup.sh`

**Step 1: Write setup script**

```bash
#!/bin/bash
kind create cluster --name kimo-test
make docker-build IMG=kimo:e2e
kind load docker-image kimo:e2e --name kimo-test
make install  # install CRDs
make deploy IMG=kimo:e2e
```

**Step 2: Write E2E test**

Test full flow: apply ChallengeTemplate → POST to API to create instance → verify pod running and phase reaches `Running` → verify networking → wait for TTL → verify phase moves through `Expiring` to `Expired` and cleanup completes.

**Step 3: Commit**

```bash
git add -A
git commit -m "test: add E2E tests with kind cluster"
```

---

### Task 23: CI Pipeline (GitHub Actions)

**Files:**
- Create: `.github/workflows/ci.yml`

**Step 1: Write CI workflow**

```yaml
name: CI
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v4

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: make test

  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: make envtest
      - run: go test ./test/integration/ -v -timeout 120s

  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - uses: helm/kind-action@v1
      - run: make docker-build IMG=kimo:e2e
      - run: kind load docker-image kimo:e2e
      - run: make install deploy IMG=kimo:e2e
      - run: go test ./test/e2e/ -v -timeout 300s
```

**Step 2: Commit**

```bash
git add -A
git commit -m "ci: add GitHub Actions workflow for lint, test, integration, e2e"
```

---

### Task 24: Sample Challenge Manifests

**Files:**
- Create: `config/samples/web-sqli-template.yaml`
- Create: `config/samples/challenge-set.yaml`
- Create: `config/samples/values-ctfd-backend.yaml`

Create example CRs and Helm values that demonstrate a container challenge with PoW and a readiness check, a ChallengeSet with scheduling, and how to point a deployment at the CTFd backend instead of the generic default. These serve as documentation and testing fixtures.

**Step 1: Write samples, commit**

```bash
git add -A
git commit -m "docs: add sample challenge manifests and backend configuration examples"
```
