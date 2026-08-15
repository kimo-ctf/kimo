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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-challenge", Namespace: "default"},
	})
	require.NoError(t, err)

	var updated kimov1alpha1.ChallengeTemplate
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "test-challenge", Namespace: "default"}, &updated))
	assert.False(t, updated.Status.Ready)
	assert.Contains(t, updated.Status.Message, "image")
}

// Deployment-managed pods only accept RestartPolicy: Always in Kubernetes;
// OnFailure/Never would fail Deployment creation in the Instance
// Controller, so the Template Controller must reject them up front.
func TestTemplateController_RejectsNonAlwaysRestartPolicy(t *testing.T) {
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
				Image:         "test:latest",
				RestartPolicy: kimov1alpha1.RestartOnFailure,
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(secret, tmpl).
		WithStatusSubresource(tmpl).
		Build()

	r := &ChallengeTemplateReconciler{Client: client, Scheme: scheme}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-challenge", Namespace: "default"},
	})
	require.NoError(t, err)

	var updated kimov1alpha1.ChallengeTemplate
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "test-challenge", Namespace: "default"}, &updated))
	assert.False(t, updated.Status.Ready)
	assert.Contains(t, updated.Status.Message, "restartPolicy")
}
