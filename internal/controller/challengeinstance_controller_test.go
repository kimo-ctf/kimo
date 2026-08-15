/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"net/http"
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

	var fence kimov1alpha1.NetworkFence
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &fence))
	assert.Equal(t, "web-sqli-team-1", fence.Spec.InstanceRef)
	require.Len(t, fence.Spec.AllowRules, 1)
	assert.Equal(t, int32(8080), fence.Spec.AllowRules[0].Port)
	require.Len(t, fence.Spec.DenyRules, 1)
	assert.Equal(t, "kimo-system", fence.Spec.DenyRules[0].To)
	assert.False(t, fence.Spec.AllowEgress)

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

// mapPodToInstance is what makes the controller re-reconcile when its
// workload Pod's readiness changes (Owns() alone can't, since the Pod is
// two hops from the ChallengeInstance: Deployment -> ReplicaSet -> Pod).
// A regression here reintroduces the up-to-15s staleness a real
// integration test caught (see test/integration/instance_test.go).
func TestMapPodToInstance(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-sqli-team-1-abcde", Namespace: "default",
			Labels: map[string]string{"kimo.io/instance": "web-sqli-team-1"},
		},
	}
	reqs := mapPodToInstance(context.Background(), pod)
	require.Len(t, reqs, 1)
	assert.Equal(t, types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, reqs[0].NamespacedName)
}

func TestMapPodToInstance_IgnoresPodsWithoutTheLabel(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated-pod", Namespace: "default"},
	}
	assert.Empty(t, mapPodToInstance(context.Background(), pod))
}
