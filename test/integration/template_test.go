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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
)

var _ = Describe("ChallengeTemplate", func() {
	ctx := context.Background()

	It("becomes ready once its flag secret and image are present", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "tmpl-flag", Namespace: "default"},
			StringData: map[string]string{"flag": "FLAG{integration}"},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, secret) })

		tmpl := &kimov1alpha1.ChallengeTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "tmpl-ready", Namespace: "default"},
			Spec: kimov1alpha1.ChallengeTemplateSpec{
				FlagSecretRef: corev1.LocalObjectReference{Name: secret.Name},
				InstanceMode:  kimov1alpha1.InstanceModePerTeam,
				TTL:           "30m",
				MaxInstances:  10,
				Container:     kimov1alpha1.ContainerSpec{Image: "example.com/challenge:v1"},
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
	})

	It("stays not-ready and reports why when the flag secret is missing", func() {
		tmpl := &kimov1alpha1.ChallengeTemplate{
			ObjectMeta: metav1.ObjectMeta{Name: "tmpl-missing-secret", Namespace: "default"},
			Spec: kimov1alpha1.ChallengeTemplateSpec{
				FlagSecretRef: corev1.LocalObjectReference{Name: "does-not-exist"},
				InstanceMode:  kimov1alpha1.InstanceModePerTeam,
				TTL:           "30m",
				MaxInstances:  10,
				Container:     kimov1alpha1.ContainerSpec{Image: "example.com/challenge:v1"},
			},
		}
		Expect(k8sClient.Create(ctx, tmpl)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, tmpl) })

		Eventually(func() string {
			var got kimov1alpha1.ChallengeTemplate
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tmpl.Name, Namespace: tmpl.Namespace}, &got); err != nil {
				return ""
			}
			return got.Status.Message
		}).Should(ContainSubstring("flag secret"))
	})
})
