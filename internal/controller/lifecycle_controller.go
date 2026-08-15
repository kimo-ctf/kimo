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

// LifecycleReconciler drives the tail of the ChallengeInstance state
// machine: Running/Unhealthy -> Expiring -> Expired, based on TTL.
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
	remaining := instance.Status.ExpiresAt.Sub(now)

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
