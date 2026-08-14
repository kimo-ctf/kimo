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

// NetworkFenceReconciler reconciles a NetworkFence object
type NetworkFenceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=networkfences,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=networkfences/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=networkfences/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile ensures a NetworkPolicy exists that fences the instance's pods
// per the allow/deny rules on the NetworkFence spec.
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

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkFenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kimov1alpha1.NetworkFence{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("networkfence").
		Complete(r)
}
