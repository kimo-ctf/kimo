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
	assert.Equal(t, "web-sqli-team-1", np.Spec.PodSelector.MatchLabels["kimo.io/instance"])

	var updated kimov1alpha1.NetworkFence
	require.NoError(t, client.Get(context.Background(),
		types.NamespacedName{Name: "web-sqli-team-1", Namespace: "default"}, &updated))
	assert.True(t, updated.Status.Applied)
}
