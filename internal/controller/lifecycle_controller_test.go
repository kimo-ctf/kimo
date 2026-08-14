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
