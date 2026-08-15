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

package integration

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
)

// createReadyTemplate creates a flag Secret + a ready ChallengeTemplate and
// registers cleanup for both, returning the template.
func createReadyTemplate(ctx context.Context, name, ttl string) *kimov1alpha1.ChallengeTemplate {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name + "-flag", Namespace: "default"},
		StringData: map[string]string{"flag": "FLAG{integration}"},
	}
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })

	tmpl := &kimov1alpha1.ChallengeTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: kimov1alpha1.ChallengeTemplateSpec{
			FlagSecretRef: corev1.LocalObjectReference{Name: secret.Name},
			InstanceMode:  kimov1alpha1.InstanceModePerTeam,
			TTL:           ttl,
			MaxInstances:  10,
			Container: kimov1alpha1.ContainerSpec{
				Image: "example.com/challenge:v1",
				Ports: []kimov1alpha1.ContainerPort{
					{Name: "http", ContainerPort: 8080, Expose: true},
				},
				UnhealthyThreshold: 3,
			},
		},
	}
	Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())
	DeferCleanup(func() { _ = k8sClient.Delete(ctx, tmpl) })

	Eventually(func() bool {
		var got kimov1alpha1.ChallengeTemplate
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tmpl.Name, Namespace: tmpl.Namespace}, &got); err != nil {
			return false
		}
		return got.Status.Ready
	}).Should(BeTrue())

	return tmpl
}

var _ = Describe("ChallengeInstance", func() {
	ctx := context.Background()

	It("creates a Deployment and Service, and reaches Creating with no Pod yet", func() {
		tmpl := createReadyTemplate(ctx, "inst-basic-tmpl", "30m")

		instance := &kimov1alpha1.ChallengeInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "inst-basic", Namespace: "default",
				Labels: map[string]string{"kimo.io/challenge": tmpl.Name, "kimo.io/team": "team-1"},
			},
			Spec: kimov1alpha1.ChallengeInstanceSpec{TemplateRef: tmpl.Name, Team: "team-1"},
		}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, instance) })

		var dep appsv1.Deployment
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &dep)
		}).Should(Succeed())
		Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/challenge:v1"))
		Expect(dep.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyAlways))
		Expect(dep.OwnerReferences).To(HaveLen(1))
		Expect(dep.OwnerReferences[0].Name).To(Equal(instance.Name))

		var svc corev1.Service
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &svc)
		}).Should(Succeed())
		Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))

		// Proves the full chain works, not just the Instance Controller in
		// isolation: it creates a NetworkFence, and the separate NetworkFence
		// Controller (a real watch + reconcile of its own) turns that into
		// an actual NetworkPolicy.
		var fence kimov1alpha1.NetworkFence
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &fence)
		}).Should(Succeed())
		Expect(fence.Spec.InstanceRef).To(Equal(instance.Name))

		var np networkingv1.NetworkPolicy
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "kimo-" + instance.Name, Namespace: "default"}, &np)
		}).Should(Succeed())
		Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("kimo.io/instance", instance.Name))

		// envtest has no kube-controller-manager/kubelet, so the Deployment
		// never gets a real Pod — Creating (not Running) is as far as this
		// can legitimately go without simulating one (see next test).
		Eventually(func() kimov1alpha1.InstancePhase {
			var got kimov1alpha1.ChallengeInstance
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &got); err != nil {
				return ""
			}
			return got.Status.Phase
		}).Should(Equal(kimov1alpha1.InstancePhaseCreating))
	})

	It("reaches Running once its Pod reports Ready, driven by a real watch+reconcile loop", func() {
		tmpl := createReadyTemplate(ctx, "inst-ready-tmpl", "30m")

		instance := &kimov1alpha1.ChallengeInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "inst-ready", Namespace: "default",
				Labels: map[string]string{"kimo.io/challenge": tmpl.Name, "kimo.io/team": "team-1"},
			},
			Spec: kimov1alpha1.ChallengeInstanceSpec{TemplateRef: tmpl.Name, Team: "team-1"},
		}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, instance) })

		Eventually(func() kimov1alpha1.InstancePhase {
			var got kimov1alpha1.ChallengeInstance
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &got)
			return got.Status.Phase
		}).Should(Equal(kimov1alpha1.InstancePhaseCreating))

		// Simulate what a real kubelet would report: a Pod matching the
		// instance's selector, Running and Ready.
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: instance.Name + "-simulated", Namespace: "default",
				Labels: map[string]string{"kimo.io/instance": instance.Name},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "challenge", Image: "example.com/challenge:v1"}},
			},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, pod) })

		pod.Status = corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())

		Eventually(func() kimov1alpha1.InstancePhase {
			var got kimov1alpha1.ChallengeInstance
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &got)
			return got.Status.Phase
		}).Should(Equal(kimov1alpha1.InstancePhaseRunning))
	})

	It("moves through Expiring to Expired once its TTL elapses, and notifies the backend", func() {
		tmpl := createReadyTemplate(ctx, "inst-ttl-tmpl", "3s")

		instance := &kimov1alpha1.ChallengeInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "inst-ttl", Namespace: "default",
				Labels: map[string]string{"kimo.io/challenge": tmpl.Name, "kimo.io/team": "team-1"},
			},
			Spec: kimov1alpha1.ChallengeInstanceSpec{TemplateRef: tmpl.Name, Team: "team-1"},
		}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, instance) })

		Eventually(func() kimov1alpha1.InstancePhase {
			var got kimov1alpha1.ChallengeInstance
			_ = k8sClient.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: "default"}, &got)
			return got.Status.Phase
		}, "20s").Should(Equal(kimov1alpha1.InstancePhaseExpired))
	})
})
