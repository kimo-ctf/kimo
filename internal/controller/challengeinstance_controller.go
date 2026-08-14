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
	"github.com/hermannchristopher/kimo/internal/integrations"
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

// ChallengeInstanceReconciler reconciles a ChallengeInstance object
type ChallengeInstanceReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Backend integrations.Backend
}

// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kimo.kimo.io,resources=challengeinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives the container lifecycle state machine: it ensures the
// owned Deployment/Service exist, then inspects the owned Pod's status to
// transition Pending -> Creating -> Running <-> Unhealthy, notifying the
// active scoring backend on every phase change.
func (r *ChallengeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var instance kimov1alpha1.ChallengeInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if terminal(instance.Status.Phase) {
		return ctrl.Result{}, nil
	}

	var tmpl kimov1alpha1.ChallengeTemplate
	if err := r.Get(ctx, types.NamespacedName{Name: instance.Spec.TemplateRef, Namespace: instance.Namespace}, &tmpl); err != nil {
		if errors.IsNotFound(err) {
			return r.transition(ctx, &instance, kimov1alpha1.InstancePhaseFailed, "template not found: "+instance.Spec.TemplateRef)
		}
		return ctrl.Result{}, err
	}

	if !tmpl.Status.Ready {
		r.transition(ctx, &instance, kimov1alpha1.InstancePhasePending, "waiting for template to be ready")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := r.ensureWorkload(ctx, &instance, &tmpl); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureExpiry(&instance, &tmpl); err != nil {
		return r.transition(ctx, &instance, kimov1alpha1.InstancePhaseFailed, err.Error())
	}

	phase, reason, err := r.determinePhase(ctx, &instance, &tmpl)
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("instance reconciled", "name", instance.Name, "phase", phase)
	return r.transition(ctx, &instance, phase, reason)
}

func terminal(p kimov1alpha1.InstancePhase) bool {
	return p == kimov1alpha1.InstancePhaseExpired || p == kimov1alpha1.InstancePhaseFailed
}

func (r *ChallengeInstanceReconciler) ensureWorkload(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) error {
	dep := r.buildDeployment(instance, tmpl)
	if err := controllerutil.SetControllerReference(instance, dep, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference: %w", err)
	}
	var existing appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &existing); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := r.Create(ctx, dep); err != nil {
			return fmt.Errorf("creating deployment: %w", err)
		}
	}

	if svc := r.buildService(instance, tmpl); svc != nil {
		if err := controllerutil.SetControllerReference(instance, svc, r.Scheme); err != nil {
			return err
		}
		var existingSvc corev1.Service
		if err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &existingSvc); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
			if err := r.Create(ctx, svc); err != nil {
				return fmt.Errorf("creating service: %w", err)
			}
		}
	}

	instance.Status.PodName = dep.Name
	return nil
}

func (r *ChallengeInstanceReconciler) ensureExpiry(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) error {
	if instance.Status.ExpiresAt != nil {
		return nil
	}
	ttl := tmpl.Spec.TTL
	if instance.Spec.TTLOverride != "" {
		ttl = instance.Spec.TTLOverride
	}
	duration, err := time.ParseDuration(ttl)
	if err != nil {
		return fmt.Errorf("invalid TTL: %s", ttl)
	}
	now := metav1.Now()
	instance.Status.StartedAt = &now
	expiresAt := metav1.NewTime(now.Add(duration))
	instance.Status.ExpiresAt = &expiresAt
	return nil
}

// determinePhase inspects the owned Pod's status to drive the lifecycle
// state machine: no pod yet -> Creating; pod ready -> Running; pod running
// but failing readiness past the template's threshold -> Unhealthy.
func (r *ChallengeInstanceReconciler) determinePhase(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) (kimov1alpha1.InstancePhase, string, error) {
	var pods corev1.PodList
	if err := r.List(ctx, &pods, client.InNamespace(instance.Namespace),
		client.MatchingLabels{"kimo.io/instance": instance.Name}); err != nil {
		return "", "", err
	}
	if len(pods.Items) == 0 {
		return kimov1alpha1.InstancePhaseCreating, "waiting for pod to be scheduled", nil
	}

	pod := pods.Items[0]
	switch pod.Status.Phase {
	case corev1.PodFailed:
		return kimov1alpha1.InstancePhaseFailed, "pod failed: " + pod.Status.Reason, nil
	case corev1.PodRunning:
		if podReady(&pod) {
			instance.Status.UnhealthyCount = 0
			return kimov1alpha1.InstancePhaseRunning, "", nil
		}
		threshold := tmpl.Spec.Container.UnhealthyThreshold
		if threshold == 0 {
			threshold = 3
		}
		instance.Status.UnhealthyCount++
		if instance.Status.UnhealthyCount >= threshold {
			return kimov1alpha1.InstancePhaseUnhealthy, "pod failing readiness checks", nil
		}
		return kimov1alpha1.InstancePhaseCreating, "waiting for pod to become ready", nil
	default:
		return kimov1alpha1.InstancePhaseCreating, "waiting for pod to start", nil
	}
}

func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// transition updates status and, on an actual phase change, notifies the
// active scoring backend.
func (r *ChallengeInstanceReconciler) transition(ctx context.Context, instance *kimov1alpha1.ChallengeInstance, phase kimov1alpha1.InstancePhase, reason string) (ctrl.Result, error) {
	changed := instance.Status.Phase != phase
	instance.Status.Phase = phase
	instance.Status.Reason = reason

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	if changed && r.Backend != nil {
		if evt, ok := eventForPhase(phase); ok {
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
	}

	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

func eventForPhase(phase kimov1alpha1.InstancePhase) (integrations.EventType, bool) {
	switch phase {
	case kimov1alpha1.InstancePhaseCreating:
		return integrations.EventCreating, true
	case kimov1alpha1.InstancePhaseRunning:
		return integrations.EventRunning, true
	case kimov1alpha1.InstancePhaseUnhealthy:
		return integrations.EventUnhealthy, true
	case kimov1alpha1.InstancePhaseFailed:
		return integrations.EventFailed, true
	default:
		return "", false
	}
}

func (r *ChallengeInstanceReconciler) buildDeployment(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *appsv1.Deployment {
	replicas := int32(1)
	labels := map[string]string{
		"kimo.io/challenge": tmpl.Name,
		"kimo.io/team":      instance.Spec.Team,
		"kimo.io/instance":  instance.Name,
	}

	c := tmpl.Spec.Container
	container := corev1.Container{
		Name:  "challenge",
		Image: c.Image,
		Env:   c.Env,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             boolPtr(true),
			ReadOnlyRootFilesystem:   boolPtr(true),
			AllowPrivilegeEscalation: boolPtr(false),
		},
	}
	for _, p := range c.Ports {
		container.Ports = append(container.Ports, corev1.ContainerPort{Name: p.Name, ContainerPort: p.ContainerPort})
	}
	if probe := buildReadinessProbe(c); probe != nil {
		container.ReadinessProbe = probe
	}

	restartPolicy := corev1.RestartPolicyOnFailure
	switch c.RestartPolicy {
	case kimov1alpha1.RestartAlways:
		restartPolicy = corev1.RestartPolicyAlways
	case kimov1alpha1.RestartNever:
		restartPolicy = corev1.RestartPolicyNever
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers:                   []corev1.Container{container},
					RestartPolicy:                restartPolicy,
					AutomountServiceAccountToken: boolPtr(false),
				},
			},
		},
	}
}

func buildReadinessProbe(c kimov1alpha1.ContainerSpec) *corev1.Probe {
	readiness := c.Readiness
	if readiness != nil && readiness.Type == kimov1alpha1.ReadinessNone {
		return nil
	}

	port := int32(0)
	if readiness != nil && readiness.Port != 0 {
		port = readiness.Port
	} else if len(c.Ports) > 0 {
		port = c.Ports[0].ContainerPort
	}
	if port == 0 {
		return nil
	}

	if readiness != nil && readiness.Type == kimov1alpha1.ReadinessHTTP {
		return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: readiness.Path, Port: intstr.FromInt32(port)},
		}}
	}
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
		TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(port)},
	}}
}

func (r *ChallengeInstanceReconciler) buildService(instance *kimov1alpha1.ChallengeInstance, tmpl *kimov1alpha1.ChallengeTemplate) *corev1.Service {
	var ports []corev1.ServicePort
	for _, p := range tmpl.Spec.Container.Ports {
		if p.Expose {
			ports = append(ports, corev1.ServicePort{Name: p.Name, Port: p.ContainerPort, TargetPort: intstr.FromInt32(p.ContainerPort)})
		}
	}
	if len(ports) == 0 {
		return nil
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: instance.Namespace},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"kimo.io/instance": instance.Name},
			Ports:    ports,
		},
	}
}

func boolPtr(b bool) *bool { return &b }

// SetupWithManager sets up the controller with the Manager.
func (r *ChallengeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kimov1alpha1.ChallengeInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("challengeinstance").
		Complete(r)
}
