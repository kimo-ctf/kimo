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

// ChallengeTemplateReconciler reconciles a ChallengeTemplate object
type ChallengeTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengetemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile validates a ChallengeTemplate (flag secret exists, container image
// set) and reports readiness plus the count of instances referencing it.
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
	// require a bare Pod, which isn't supported yet.
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

// SetupWithManager sets up the controller with the Manager.
func (r *ChallengeTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kimov1alpha1.ChallengeTemplate{}).
		Named("challengetemplate").
		Complete(r)
}
