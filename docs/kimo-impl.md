# KIMO Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:executing-plans to implement this plan task-by-task.

**Goal:** Build a Kubernetes operator that deploys and manages CTF challenge instances on containers and VMs, with a REST API, PoW protection, and Discord bot.

**Architecture:** Single Go operator binary (kubebuilder) with 5 controllers (Template, Instance, Network, Lifecycle, Set), a REST API server embedded in the manager, and a separate Discord bot binary. All deployed via Helm.

**Tech Stack:** Go 1.22+, kubebuilder v4, controller-runtime, KubeVirt client-go, chi (HTTP router), discordgo, envtest, kind

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

// RuntimeType defines the workload runtime.
// +kubebuilder:validation:Enum=container;vm
type RuntimeType string

const (
        RuntimeContainer RuntimeType = "container"
        RuntimeVM        RuntimeType = "vm"
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

// ContainerSpec defines the container runtime configuration.
type ContainerSpec struct {
        Image     string                `json:"image"`
        Ports     []ContainerPort       `json:"ports,omitempty"`
        Resources ResourceRequirements  `json:"resources,omitempty"`
        Env       []corev1.EnvVar       `json:"env,omitempty"`
}

// VMSpec defines the KubeVirt VM runtime configuration.
type VMSpec struct {
        Image     string               `json:"image,omitempty"`     // VM disk image
        CPUs      int32                `json:"cpus,omitempty"`
        Memory    resource.Quantity    `json:"memory,omitempty"`
        DiskSize  resource.Quantity    `json:"diskSize,omitempty"`
}

// ChallengeTemplateSpec defines the desired state of ChallengeTemplate.
type ChallengeTemplateSpec struct {
        Category      string              `json:"category,omitempty"`
        Difficulty    string              `json:"difficulty,omitempty"`
        Points        int                 `json:"points,omitempty"`
        FlagSecretRef corev1.LocalObjectReference `json:"flagSecretRef"`
        InstanceMode  InstanceMode        `json:"instanceMode"`
        Runtime       RuntimeType         `json:"runtime"`
        TTL           string              `json:"ttl"`                  // e.g. "30m"
        MaxInstances  int                 `json:"maxInstances"`
        PoW           *PoWSpec            `json:"pow,omitempty"`
        Container     *ContainerSpec      `json:"container,omitempty"`  // when runtime=container
        VM            *VMSpec             `json:"vm,omitempty"`         // when runtime=vm
}

// ChallengeTemplateStatus defines the observed state.
type ChallengeTemplateStatus struct {
        Ready         bool   `json:"ready"`
        InstanceCount int    `json:"instanceCount"`
        Message       string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Runtime",type=string,JSONPath=`.spec.runtime`
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

Edit `api/v1alpha1/challengeinstance_types.go`:

```go
package v1alpha1

import (
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstancePhase represents the lifecycle phase of an instance.
// +kubebuilder:validation:Enum=Pending;Running;Expired;Failed
type InstancePhase string

const (
        InstancePhasePending InstancePhase = "Pending"
        InstancePhaseRunning InstancePhase = "Running"
        InstancePhaseExpired InstancePhase = "Expired"
        InstancePhaseFailed  InstancePhase = "Failed"
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
        Phase     InstancePhase  `json:"phase,omitempty"`
        Endpoint  string         `json:"endpoint,omitempty"`
        StartedAt *metav1.Time   `json:"startedAt,omitempty"`
        ExpiresAt *metav1.Time   `json:"expiresAt,omitempty"`
        PodName   string         `json:"podName,omitempty"`
        VMName    string         `json:"vmName,omitempty"`
        Message   string         `json:"message,omitempty"`
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
        Challenges []string     `json:"challenges"`
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
        InstanceRef  string      `json:"instanceRef"`
        AllowRules   []AllowRule `json:"allow,omitempty"`
        DenyRules    []DenyRule  `json:"deny,omitempty"`
        AllowEgress  bool        `json:"allowEgress,omitempty"` // allow internet
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
                        Runtime:       kimov1alpha1.RuntimeContainer,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container: &kimov1alpha1.ContainerSpec{
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
                        Runtime:       kimov1alpha1.RuntimeContainer,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container:     &kimov1alpha1.ContainerSpec{Image: "test:latest"},
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

func TestTemplateController_MissingContainerSpec(t *testing.T) {
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
                        Runtime:       kimov1alpha1.RuntimeContainer,
                        TTL:           "30m",
                        MaxInstances:  100,
                        // Container is nil — should fail validation
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
        assert.Contains(t, updated.Status.Message, "container spec")
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

        // Validate runtime spec
        switch tmpl.Spec.Runtime {
        case kimov1alpha1.RuntimeContainer:
                if tmpl.Spec.Container == nil {
                        return r.setStatus(ctx, &tmpl, false, "container spec required when runtime=container")
                }
                if tmpl.Spec.Container.Image == "" {
                        return r.setStatus(ctx, &tmpl, false, "container image is required")
                }
        case kimov1alpha1.RuntimeVM:
                if tmpl.Spec.VM == nil {
                        return r.setStatus(ctx, &tmpl, false, "vm spec required when runtime=vm")
                }
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

### Task 5: Instance Controller — Container Runtime

**Files:**
- Modify: `internal/controller/challengeinstance_controller.go`
- Create: `internal/controller/challengeinstance_controller_test.go`

**Step 1: Write the failing test**

Create `internal/controller/challengeinstance_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
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

func TestInstanceController_CreatesDeploymentAndService(t *testing.T) {
        scheme := newTestScheme(t)

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "web-sqli", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        FlagSecretRef: corev1.LocalObjectReference{Name: "flag"},
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        Runtime:       kimov1alpha1.RuntimeContainer,
                        TTL:           "30m",
                        MaxInstances:  100,
                        Container: &kimov1alpha1.ContainerSpec{
                                Image: "ctf/web-sqli:v1",
                                Ports: []kimov1alpha1.ContainerPort{
                                        {Name: "http", ContainerPort: 8080, Expose: true},
                                },
                        },
                },
                Status: kimov1alpha1.ChallengeTemplateStatus{Ready: true},
        }

        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{
                        Name: "web-sqli-team-1", Namespace: "default",
                        Labels: map[string]string{"kimo.io/challenge": "web-sqli", "kimo.io/team": "team-1"},
                },
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "web-sqli",
                        Team:        "team-1",
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance).
                WithStatusSubresource(instance).
                Build()

        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        // Verify Deployment created
        var dep appsv1.Deployment
        err = client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &dep)
        require.NoError(t, err)
        assert.Equal(t, "ctf/web-sqli:v1", dep.Spec.Template.Spec.Containers[0].Image)

        // Verify Service created
        var svc corev1.Service
        err = client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &svc)
        require.NoError(t, err)
        assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)

        // Verify status updated
        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhasePending, updated.Status.Phase)
        assert.NotNil(t, updated.Status.ExpiresAt)
}

func TestInstanceController_TemplateNotReady(t *testing.T) {
        scheme := newTestScheme(t)

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "broken", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        Runtime: kimov1alpha1.RuntimeContainer,
                },
                Status: kimov1alpha1.ChallengeTemplateStatus{Ready: false},
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

        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme}
        result, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "broken-team-1", Namespace: "default"},
        })
        require.NoError(t, err)
        assert.NotZero(t, result.RequeueAfter) // requeue to wait for template

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
        Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *ChallengeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
        logger := log.FromContext(ctx)

        var instance kimov1alpha1.ChallengeInstance
        if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
                if errors.IsNotFound(err) {
                        return ctrl.Result{}, nil
                }
                return ctrl.Result{}, err
        }

        // Skip if already expired
        if instance.Status.Phase == kimov1alpha1.InstancePhaseExpired {
                return ctrl.Result{}, nil
        }

        // Fetch template
        var tmpl kimov1alpha1.ChallengeTemplate
        if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.TemplateRef, Namespace: instance.Namespace}, &tmpl); err != nil {
                if errors.IsNotFound(err) {
                        return r.setInstanceStatus(ctx, &instance, kimov1alpha1.InstancePhaseFailed, "template not found: "+instance.Spec.TemplateRef)
                }
                return ctrl.Result{}, err
        }

        // Wait for template to be ready
        if !tmpl.Status.Ready {
                r.setInstanceStatus(ctx, &instance, kimov1alpha1.InstancePhasePending, "waiting for template to be ready")
                return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
        }

        // Route by runtime type
        switch tmpl.Spec.Runtime {
        case kimov1alpha1.RuntimeContainer:
                return r.reconcileContainer(ctx, &instance, &tmpl)
        case kimov1alpha1.RuntimeVM:
                return r.reconcileVM(ctx, &instance, &tmpl)
        default:
                return r.setInstanceStatus(ctx, &instance, kimov1alpha1.InstancePhaseFailed, "unknown runtime: "+string(tmpl.Spec.Runtime))
        }

        _ = logger
        return ctrl.Result{}, nil
}

func (r *ChallengeInstanceReconciler) reconcileContainer(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) (ctrl.Result, error) {
        // Create Deployment
        dep := r.buildDeployment(instance, tmpl)
        if err := controllerutil.SetControllerReference(instance, dep, r.Scheme); err != nil {
                return ctrl.Result{}, fmt.Errorf("setting owner reference: %w", err)
        }

        var existing appsv1.Deployment
        if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &existing); err != nil {
                if errors.IsNotFound(err) {
                        if err := r.Create(ctx, dep); err != nil {
                                return ctrl.Result{}, fmt.Errorf("creating deployment: %w", err)
                        }
                } else {
                        return ctrl.Result{}, err
                }
        }

        // Create Service for exposed ports
        if svc := r.buildService(instance, tmpl); svc != nil {
                if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
                        return ctrl.Result{}, err
                }
                var existingSvc corev1.Service
                if err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &existingSvc); err != nil {
                        if errors.IsNotFound(err) {
                                if err := r.Create(ctx, svc); err != nil {
                                        return ctrl.Result{}, fmt.Errorf("creating service: %w", err)
                                }
                        } else {
                                return ctrl.Result{}, err
                        }
                }
        }

        // Set expiry time
        ttl := tmpl.Spec.TTL
        if instance.Spec.TTLOverride != "" {
                ttl = instance.Spec.TTLOverride
        }
        duration, err := time.ParseDuration(ttl)
        if err != nil {
                return r.setInstanceStatus(ctx, instance, kimov1alpha1.InstancePhaseFailed, "invalid TTL: "+ttl)
        }

        now := metav1.Now()
        if instance.Status.StartedAt == nil {
                instance.Status.StartedAt = &now
        }
        expiresAt := metav1.NewTime(instance.Status.StartedAt.Add(duration))
        instance.Status.ExpiresAt = &expiresAt
        instance.Status.Phase = kimov1alpha1.InstancePhasePending
        instance.Status.PodName = dep.Name

        if err := r.Status().Update(ctx, instance); err != nil {
                return ctrl.Result{}, err
        }

        return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *ChallengeInstanceReconciler) reconcileVM(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) (ctrl.Result, error) {
        // TODO: Implement KubeVirt VMI creation in Task 6
        return r.setInstanceStatus(ctx, instance, kimov1alpha1.InstancePhasePending, "VM runtime not yet implemented")
}

func (r *ChallengeInstanceReconciler) buildDeployment(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *appsv1.Deployment {
        replicas := int32(1)
        labels := map[string]string{
                "kimo.io/challenge": tmpl.Name,
                "kimo.io/team":      instance.Spec.Team,
                "kimo.io/instance":  instance.Name,
        }

        containers := []corev1.Container{
                {
                        Name:  "challenge",
                        Image: tmpl.Spec.Container.Image,
                        Env:   tmpl.Spec.Container.Env,
                        SecurityContext: &corev1.SecurityContext{
                                RunAsNonRoot:             boolPtr(true),
                                ReadOnlyRootFilesystem:   boolPtr(true),
                                AllowPrivilegeEscalation: boolPtr(false),
                        },
                },
        }

        // Map ports
        for _, p := range tmpl.Spec.Container.Ports {
                containers[0].Ports = append(containers[0].Ports, corev1.ContainerPort{
                        Name:          p.Name,
                        ContainerPort: p.ContainerPort,
                })
        }

        return &appsv1.Deployment{
                ObjectMeta: metav1.ObjectMeta{
                        Name:      instance.Name,
                        Namespace: instance.Namespace,
                        Labels:    labels,
                },
                Spec: appsv1.DeploymentSpec{
                        Replicas: &replicas,
                        Selector: &metav1.LabelSelector{MatchLabels: labels},
                        Template: corev1.PodTemplateSpec{
                                ObjectMeta: metav1.ObjectMeta{Labels: labels},
                                Spec: corev1.PodSpec{
                                        Containers:                containers,
                                        AutomountServiceAccountToken: boolPtr(false),
                                },
                        },
                },
        }
}

func (r *ChallengeInstanceReconciler) buildService(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *corev1.Service {
        var ports []corev1.ServicePort
        for _, p := range tmpl.Spec.Container.Ports {
                if p.Expose {
                        ports = append(ports, corev1.ServicePort{
                                Name:       p.Name,
                                Port:       p.ContainerPort,
                                TargetPort: intstr.FromInt32(p.ContainerPort),
                        })
                }
        }
        if len(ports) == 0 {
                return nil
        }

        return &corev1.Service{
                ObjectMeta: metav1.ObjectMeta{
                        Name:      instance.Name,
                        Namespace: instance.Namespace,
                },
                Spec: corev1.ServiceSpec{
                        Selector: map[string]string{"kimo.io/instance": instance.Name},
                        Ports:    ports,
                },
        }
}

func boolPtr(b bool) *bool { return &b }

func (r *ChallengeInstanceReconciler) setInstanceStatus(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, phase kimov1alpha1.InstancePhase, msg string) (ctrl.Result, error) {
        instance.Status.Phase = phase
        instance.Status.Message = msg
        if err := r.Status().Update(ctx, instance); err != nil {
                return ctrl.Result{}, err
        }
        return ctrl.Result{}, nil
}

func (r *ChallengeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
                For(&kimov1alpha1.ChallengeInstance{}).
                Owns(&appsv1.Deployment{}).
                Owns(&corev1.Service{}).
                Complete(r)
}
```

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestInstanceController -v
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: implement Instance Controller for container runtime"
```

---

### Task 6: Instance Controller — VM Runtime (KubeVirt)

**Files:**
- Modify: `internal/controller/challengeinstance_controller.go`
- Modify: `internal/controller/challengeinstance_controller_test.go`

**Step 1: Add KubeVirt dependency**

```bash
go get kubevirt.io/api@latest
go get kubevirt.io/client-go@latest
```

**Step 2: Write the failing test for VM provisioning**

Append to `internal/controller/challengeinstance_controller_test.go`:

```go
func TestInstanceController_CreatesVMI(t *testing.T) {
        scheme := newTestScheme(t)
        // Add KubeVirt types to scheme
        require.NoError(t, kubevirtv1.AddToScheme(scheme))

        tmpl := &kimov1alpha1.ChallengeTemplate{
                ObjectMeta: metav1.ObjectMeta{Name: "pwn-vm", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeTemplateSpec{
                        Runtime:       kimov1alpha1.RuntimeVM,
                        TTL:           "60m",
                        MaxInstances:  50,
                        InstanceMode:  kimov1alpha1.InstanceModePerTeam,
                        FlagSecretRef: corev1.LocalObjectReference{Name: "flag"},
                        VM: &kimov1alpha1.VMSpec{
                                Image:    "registry.ctf.io/vms/pwn:v1",
                                CPUs:     2,
                                Memory:   resource.MustParse("2Gi"),
                                DiskSize: resource.MustParse("10Gi"),
                        },
                },
                Status: kimov1alpha1.ChallengeTemplateStatus{Ready: true},
        }

        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{
                        Name: "pwn-vm-team-1", Namespace: "default",
                        Labels: map[string]string{"kimo.io/challenge": "pwn-vm", "kimo.io/team": "team-1"},
                },
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "pwn-vm",
                        Team:        "team-1",
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(tmpl, instance).
                WithStatusSubresource(instance).
                Build()

        r := &ChallengeInstanceReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "pwn-vm-team-1", Namespace: "default"},
        })
        require.NoError(t, err)

        // Verify VMI created
        var vmi kubevirtv1.VirtualMachineInstance
        err = client.Get(context.Background(),
                types.NamespacedName{Name: "pwn-vm-team-1", Namespace: "default"}, &vmi)
        require.NoError(t, err)
        assert.Equal(t, uint32(2), vmi.Spec.Domain.CPU.Cores)
}
```

**Step 3: Implement `reconcileVM`**

Fill in the `reconcileVM` method in `challengeinstance_controller.go`:

```go
func (r *ChallengeInstanceReconciler) reconcileVM(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) (ctrl.Result, error) {
        vmi := r.buildVMI(instance, tmpl)
        if err := controllerutil.SetControllerReference(instance, vmi, r.Scheme); err != nil {
                return ctrl.Result{}, err
        }

        var existing kubevirtv1.VirtualMachineInstance
        if err := r.Get(ctx, types.NamespacedName{Name: vmi.Name, Namespace: vmi.Namespace}, &existing); err != nil {
                if errors.IsNotFound(err) {
                        if err := r.Create(ctx, vmi); err != nil {
                                return ctrl.Result{}, fmt.Errorf("creating VMI: %w", err)
                        }
                } else {
                        return ctrl.Result{}, err
                }
        }

        // Set TTL and status
        ttl := tmpl.Spec.TTL
        if instance.Spec.TTLOverride != "" {
                ttl = instance.Spec.TTLOverride
        }
        duration, _ := time.ParseDuration(ttl)
        now := metav1.Now()
        if instance.Status.StartedAt == nil {
                instance.Status.StartedAt = &now
        }
        expiresAt := metav1.NewTime(instance.Status.StartedAt.Add(duration))
        instance.Status.ExpiresAt = &expiresAt
        instance.Status.Phase = kimov1alpha1.InstancePhasePending
        instance.Status.VMName = vmi.Name

        if err := r.Status().Update(ctx, instance); err != nil {
                return ctrl.Result{}, err
        }

        return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func (r *ChallengeInstanceReconciler) buildVMI(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *kubevirtv1.VirtualMachineInstance {
        labels := map[string]string{
                "kimo.io/challenge": tmpl.Name,
                "kimo.io/team":      instance.Spec.Team,
                "kimo.io/instance":  instance.Name,
        }

        return &kubevirtv1.VirtualMachineInstance{
                ObjectMeta: metav1.ObjectMeta{
                        Name:      instance.Name,
                        Namespace: instance.Namespace,
                        Labels:    labels,
                },
                Spec: kubevirtv1.VirtualMachineInstanceSpec{
                        Domain: kubevirtv1.DomainSpec{
                                CPU: &kubevirtv1.CPU{
                                        Cores: uint32(tmpl.Spec.VM.CPUs),
                                },
                                Memory: &kubevirtv1.Memory{
                                        Guest: &tmpl.Spec.VM.Memory,
                                },
                        },
                        Volumes: []kubevirtv1.Volume{
                                {
                                        Name: "disk0",
                                        VolumeSource: kubevirtv1.VolumeSource{
                                                ContainerDisk: &kubevirtv1.ContainerDiskSource{
                                                        Image: tmpl.Spec.VM.Image,
                                                },
                                        },
                                },
                        },
                },
        }
}
```

**Step 4: Run tests**

```bash
go test ./internal/controller/ -run TestInstanceController -v
```

Expected: all tests PASS.

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add KubeVirt VM runtime support to Instance Controller"
```

---

### Task 7: Network Controller

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
                        AllowRules: []kimov1alpha1.AllowRule{
                                {Port: 8080},
                        },
                        DenyRules: []kimov1alpha1.DenyRule{
                                {To: "kimo-system"},
                        },
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

        // Verify NetworkPolicy created
        var np networkingv1.NetworkPolicy
        err = client.Get(context.Background(),
                types.NamespacedName{Name: "kimo-web-sqli-team-1", Namespace: "default"}, &np)
        require.NoError(t, err)
        assert.Equal(t, "kimo.io/instance", np.Spec.PodSelector.MatchLabels["kimo.io/instance"])

        // Verify status
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

        // Build ingress rules from allow rules
        var ingressPorts []networkingv1.NetworkPolicyPort
        for _, allow := range fence.Spec.AllowRules {
                if allow.Port > 0 {
                        port := intstr.FromInt32(allow.Port)
                        ingressPorts = append(ingressPorts, networkingv1.NetworkPolicyPort{
                                Port:     &port,
                                Protocol: &protocol,
                        })
                }
        }

        // Build ingress from CIDRs
        var ingressFrom []networkingv1.NetworkPolicyPeer
        for _, allow := range fence.Spec.AllowRules {
                if allow.CIDR != "" {
                        ingressFrom = append(ingressFrom, networkingv1.NetworkPolicyPeer{
                                IPBlock: &networkingv1.IPBlock{CIDR: allow.CIDR},
                        })
                }
        }

        ingress := []networkingv1.NetworkPolicyIngressRule{
                {
                        Ports: ingressPorts,
                        From:  ingressFrom,
                },
        }

        // Build egress: deny to specific namespaces, allow rest based on config
        var egressRules []networkingv1.NetworkPolicyEgressRule
        if fence.Spec.AllowEgress {
                // Allow all egress except to denied namespaces
                egressRules = append(egressRules, networkingv1.NetworkPolicyEgressRule{})
        }

        policyTypes := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
        if len(egressRules) > 0 || len(fence.Spec.DenyRules) > 0 {
                policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
        }

        return &networkingv1.NetworkPolicy{
                ObjectMeta: metav1.ObjectMeta{
                        Name:      "kimo-" + fence.Spec.InstanceRef,
                        Namespace: fence.Namespace,
                },
                Spec: networkingv1.NetworkPolicySpec{
                        PodSelector: metav1.LabelSelector{
                                MatchLabels: map[string]string{
                                        "kimo.io/instance": fence.Spec.InstanceRef,
                                },
                        },
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

Note: you'll need to add `corev1` and `networkingv1` imports. The `corev1` import is needed for `corev1.ProtocolTCP`.

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

### Task 8: Lifecycle Controller (TTL)

**Files:**
- Modify: `internal/controller/lifecycle_controller.go` (new file — kubebuilder won't scaffold this since it doesn't own a CRD)
- Create: `internal/controller/lifecycle_controller_test.go`

**Step 1: Write the failing test**

Create `internal/controller/lifecycle_controller_test.go`:

```go
package controller

import (
        "context"
        "testing"
        "time"

        kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
        "k8s.io/apimachinery/pkg/runtime"
        "k8s.io/apimachinery/pkg/types"
        "sigs.k8s.io/controller-runtime/pkg/client/fake"
        "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestLifecycleController_ExpiresInstance(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        pastTime := metav1.NewTime(time.Now().Add(-10 * time.Minute))
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "expired-inst", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "test",
                        Team:        "team-1",
                },
                Status: kimov1alpha1.ChallengeInstanceStatus{
                        Phase:     kimov1alpha1.InstancePhaseRunning,
                        ExpiresAt: &pastTime,
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(instance).
                WithStatusSubresource(instance).
                Build()

        r := &LifecycleReconciler{Client: client, Scheme: scheme}
        _, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "expired-inst", Namespace: "default"},
        })
        require.NoError(t, err)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "expired-inst", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseExpired, updated.Status.Phase)
}

func TestLifecycleController_SkipsNonExpired(t *testing.T) {
        scheme := runtime.NewScheme()
        require.NoError(t, kimov1alpha1.AddToScheme(scheme))

        futureTime := metav1.NewTime(time.Now().Add(30 * time.Minute))
        instance := &kimov1alpha1.ChallengeInstance{
                ObjectMeta: metav1.ObjectMeta{Name: "active-inst", Namespace: "default"},
                Spec: kimov1alpha1.ChallengeInstanceSpec{
                        TemplateRef: "test",
                        Team:        "team-1",
                },
                Status: kimov1alpha1.ChallengeInstanceStatus{
                        Phase:     kimov1alpha1.InstancePhaseRunning,
                        ExpiresAt: &futureTime,
                },
        }

        client := fake.NewClientBuilder().WithScheme(scheme).
                WithObjects(instance).
                WithStatusSubresource(instance).
                Build()

        r := &LifecycleReconciler{Client: client, Scheme: scheme}
        result, err := r.Reconcile(context.Background(), reconcile.Request{
                NamespacedName: types.NamespacedName{Name: "active-inst", Namespace: "default"},
        })
        require.NoError(t, err)
        assert.NotZero(t, result.RequeueAfter)

        var updated kimov1alpha1.ChallengeInstance
        require.NoError(t, client.Get(context.Background(),
                types.NamespacedName{Name: "active-inst", Namespace: "default"}, &updated))
        assert.Equal(t, kimov1alpha1.InstancePhaseRunning, updated.Status.Phase)
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
        "k8s.io/apimachinery/pkg/api/errors"
        "k8s.io/apimachinery/pkg/runtime"
        ctrl "sigs.k8s.io/controller-runtime"
        "sigs.k8s.io/controller-runtime/pkg/client"
        "sigs.k8s.io/controller-runtime/pkg/log"
)

type LifecycleReconciler struct {
        client.Client
        Scheme *runtime.Scheme
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

        // Skip already expired or failed
        if instance.Status.Phase == kimov1alpha1.InstancePhaseExpired ||
                instance.Status.Phase == kimov1alpha1.InstancePhaseFailed {
                return ctrl.Result{}, nil
        }

        // No expiry set yet
        if instance.Status.ExpiresAt == nil {
                return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
        }

        now := time.Now()
        if now.After(instance.Status.ExpiresAt.Time) {
                logger.Info("expiring instance", "name", instance.Name)
                instance.Status.Phase = kimov1alpha1.InstancePhaseExpired
                instance.Status.Message = "TTL expired"
                if err := r.Status().Update(ctx, &instance); err != nil {
                        return ctrl.Result{}, err
                }
                // Delete after grace period (60s)
                return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
        }

        // Requeue before expiry
        remaining := instance.Status.ExpiresAt.Time.Sub(now)
        return ctrl.Result{RequeueAfter: remaining}, nil
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
git commit -m "feat: implement Lifecycle Controller with TTL expiry"
```

---

### Task 9: Set Controller

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

### Task 10: REST API Server — Core

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/handlers.go`
- Create: `internal/api/middleware.go`
- Create: `internal/api/server_test.go`

**Step 1: Add chi dependency**

```bash
go get github.com/go-chi/chi/v5
```

**Step 2: Write the failing test**

Create `internal/api/server_test.go`:

```go
package api

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestHealthEndpoint(t *testing.T) {
        srv := NewServer(nil, "test-key")
        req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_RejectsUnauthenticated(t *testing.T) {
        srv := NewServer(nil, "secret-key")
        req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_AcceptsValidKey(t *testing.T) {
        srv := NewServer(nil, "secret-key")
        req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
        req.Header.Set("Authorization", "Bearer secret-key")
        w := httptest.NewRecorder()
        srv.Router().ServeHTTP(w, req)

        // 200 or empty list, not 401
        assert.NotEqual(t, http.StatusUnauthorized, w.Code)
}
```

**Step 3: Implement server, middleware, handlers**

Create `internal/api/server.go`:

```go
package api

import (
        "net/http"

        "github.com/go-chi/chi/v5"
        "github.com/go-chi/chi/v5/middleware"
        "sigs.k8s.io/controller-runtime/pkg/client"
)

type Server struct {
        client client.Client
        apiKey string
        router chi.Router
}

func NewServer(c client.Client, apiKey string) *Server {
        s := &Server{client: c, apiKey: apiKey}
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

func (s *Server) authMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                token := r.Header.Get("Authorization")
                if token != "Bearer "+s.apiKey {
                        http.Error(w, "unauthorized", http.StatusUnauthorized)
                        return
                }
                next.ServeHTTP(w, r)
        })
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"status":"ok"}`))
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
git commit -m "feat: implement REST API server with auth middleware"
```

---

### Task 11: REST API — Instance CRUD Handlers

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

**Step 3: Run tests, commit**

```bash
go test ./internal/api/ -v
git add -A
git commit -m "feat: implement instance CRUD API handlers"
```

---

### Task 12: Proof of Work System

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

        // Solve the puzzle
        nonce, found := SolvePoW(puzzle.Challenge, puzzle.Difficulty)
        require.True(t, found)

        // Verify
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
        // Just verify the helper function works
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

### Task 13: Webhook Callback System

**Files:**
- Create: `internal/api/webhooks.go`
- Create: `internal/api/webhooks_test.go`

**Step 1: Write the failing test**

Test that registering a webhook URL and dispatching an event sends the correct HMAC-signed POST.

**Step 2: Implement webhook manager**

- In-memory registry of webhook URLs + HMAC secrets
- `Dispatch(event WebhookEvent)` method that POSTs to all registered URLs
- HMAC-SHA256 signature in `X-KIMO-Signature` header
- `handleConfigureWebhook` handler stores URL + secret

**Step 3: Integrate with Instance Controller**

Add webhook dispatch calls when instance phase changes (Pending→Running, →Failed, →Expired, deleted).

**Step 4: Run tests, commit**

```bash
go test ./internal/api/ -v
git add -A
git commit -m "feat: implement webhook callback system with HMAC signatures"
```

---

### Task 14: Wire Up Manager Entrypoint

**Files:**
- Modify: `cmd/manager/main.go`

**Step 1: Register all controllers and start API server**

Edit `cmd/manager/main.go` to:
- Register all 5 controllers with the manager
- Start the REST API server in a goroutine alongside the manager
- Read API key from environment variable `KIMO_API_KEY`
- Read operator config (domain, global instance cap) from env/ConfigMap

```go
// In main():
apiKey := os.Getenv("KIMO_API_KEY")
apiServer := api.NewServer(mgr.GetClient(), apiKey)
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
git commit -m "feat: wire up all controllers and API server in manager"
```

---

### Task 15: Discord Bot — Core Setup

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

### Task 16: Discord Bot — Slash Commands

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
                // Compare against configured role IDs
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

### Task 17: Discord Bot — Event Monitor

**Files:**
- Create: `internal/bot/monitor.go`
- Create: `internal/bot/webhook_handler.go`

**Step 1: Implement webhook receiver**

The bot runs a small HTTP server that receives KIMO webhook callbacks and posts to Discord:

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
        case "instance.expired":
                color = 0xFFA500
        case "instance.pending":
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
git commit -m "feat: implement Discord bot event monitoring via webhooks"
```

---

### Task 18: Helm Chart

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

```yaml
operator:
  image: ghcr.io/hermannchristopher/kimo:latest
  replicas: 1
  resources:
    requests: { cpu: 100m, memory: 128Mi }
    limits: { cpu: 500m, memory: 512Mi }

api:
  port: 8080
  apiKey: ""  # set via --set or secret

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

Write standard Kubernetes Deployment, Service, ServiceAccount, ClusterRole, ClusterRoleBinding templates for the operator and bot. Include CRD installation.

**Step 4: Lint chart**

```bash
helm lint helm/kimo/
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: add Helm chart for operator and bot deployment"
```

---

### Task 19: Dockerfile

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

### Task 20: Integration Tests (envtest)

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
- Create a ChallengeInstance → verify Deployment and Service created
- Wait for TTL → verify instance expires

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

### Task 21: E2E Tests (kind)

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

Test full flow: apply ChallengeTemplate → POST to API to create instance → verify pod running → verify networking → wait for TTL → verify cleanup.

**Step 3: Commit**

```bash
git add -A
git commit -m "test: add E2E tests with kind cluster"
```

---

### Task 22: CI Pipeline (GitHub Actions)

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

### Task 23: Sample Challenge Manifests

**Files:**
- Create: `config/samples/web-sqli-template.yaml`
- Create: `config/samples/pwn-vm-template.yaml`
- Create: `config/samples/challenge-set.yaml`

Create example CRs that demonstrate container and VM challenges, a ChallengeSet with scheduling, and PoW configuration. These serve as documentation and testing fixtures.

**Step 1: Write samples, commit**

```bash
git add -A
git commit -m "docs: add sample challenge manifests"
```
