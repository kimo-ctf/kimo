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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ChallengeSetReconciler reconciles a ChallengeSet object
type ChallengeSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengesets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengesets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengesets/finalizers,verbs=update

const defaultSetRequeue = 30 * time.Second

// Reconcile activates a ChallengeSet when the current time falls within its
// schedule window, and requeues at the next boundary (start or end) so the
// set flips active/inactive without needing external triggers. A set with
// no schedule is always active.
func (r *ChallengeSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var set kimov1alpha1.ChallengeSet
	if err := r.Get(ctx, req.NamespacedName, &set); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if set.Spec.Schedule == nil {
		return r.setStatus(ctx, &set, true, "", defaultSetRequeue)
	}

	startAt, err := time.Parse(time.RFC3339, set.Spec.Schedule.StartAt)
	if err != nil {
		return r.setStatus(ctx, &set, false, "invalid schedule.startAt: "+err.Error(), defaultSetRequeue)
	}
	endAt, err := time.Parse(time.RFC3339, set.Spec.Schedule.EndAt)
	if err != nil {
		return r.setStatus(ctx, &set, false, "invalid schedule.endAt: "+err.Error(), defaultSetRequeue)
	}

	now := time.Now()
	switch {
	case now.Before(startAt):
		logger.Info("challenge set not yet active", "name", set.Name, "startAt", startAt)
		return r.setStatus(ctx, &set, false, "waiting for schedule to start", startAt.Sub(now))
	case now.Before(endAt):
		logger.Info("challenge set active", "name", set.Name, "endAt", endAt)
		return r.setStatus(ctx, &set, true, "", endAt.Sub(now))
	default:
		logger.Info("challenge set schedule ended", "name", set.Name, "endAt", endAt)
		return r.setStatus(ctx, &set, false, "schedule ended", defaultSetRequeue)
	}
}

func (r *ChallengeSetReconciler) setStatus(ctx context.Context, set *kimov1alpha1.ChallengeSet, active bool, msg string, requeueAfter time.Duration) (ctrl.Result, error) {
	set.Status.Active = active
	set.Status.Message = msg
	if err := r.Status().Update(ctx, set); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ChallengeSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kimov1alpha1.ChallengeSet{}).
		Named("challengeset").
		Complete(r)
}
