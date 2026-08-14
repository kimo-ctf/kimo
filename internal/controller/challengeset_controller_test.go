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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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

func TestSetController_InactiveBeforeSchedule(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kimov1alpha1.AddToScheme(scheme))

	now := time.Now()
	set := &kimov1alpha1.ChallengeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "round-2", Namespace: "default"},
		Spec: kimov1alpha1.ChallengeSetSpec{
			Challenges: []string{"web-sqli"},
			Schedule: &kimov1alpha1.ScheduleSpec{
				StartAt: now.Add(1 * time.Hour).Format(time.RFC3339),
				EndAt:   now.Add(2 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(set).
		WithStatusSubresource(set).
		Build()

	r := &ChallengeSetReconciler{Client: client, Scheme: scheme}
	result, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "round-2", Namespace: "default"},
	})
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter)

	var updated kimov1alpha1.ChallengeSet
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "round-2", Namespace: "default"}, &updated))
	assert.False(t, updated.Status.Active)
}

func TestSetController_InactiveAfterSchedule(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kimov1alpha1.AddToScheme(scheme))

	now := time.Now()
	set := &kimov1alpha1.ChallengeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "round-3", Namespace: "default"},
		Spec: kimov1alpha1.ChallengeSetSpec{
			Challenges: []string{"web-sqli"},
			Schedule: &kimov1alpha1.ScheduleSpec{
				StartAt: now.Add(-2 * time.Hour).Format(time.RFC3339),
				EndAt:   now.Add(-1 * time.Hour).Format(time.RFC3339),
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(set).
		WithStatusSubresource(set).
		Build()

	r := &ChallengeSetReconciler{Client: client, Scheme: scheme}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "round-3", Namespace: "default"},
	})
	require.NoError(t, err)

	var updated kimov1alpha1.ChallengeSet
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "round-3", Namespace: "default"}, &updated))
	assert.False(t, updated.Status.Active)
	assert.Contains(t, updated.Status.Message, "ended")
}

func TestSetController_ActiveWithNoSchedule(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, kimov1alpha1.AddToScheme(scheme))

	set := &kimov1alpha1.ChallengeSet{
		ObjectMeta: metav1.ObjectMeta{Name: "round-4", Namespace: "default"},
		Spec:       kimov1alpha1.ChallengeSetSpec{Challenges: []string{"web-sqli"}},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(set).
		WithStatusSubresource(set).
		Build()

	r := &ChallengeSetReconciler{Client: client, Scheme: scheme}
	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "round-4", Namespace: "default"},
	})
	require.NoError(t, err)

	var updated kimov1alpha1.ChallengeSet
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "round-4", Namespace: "default"}, &updated))
	assert.True(t, updated.Status.Active)
}
