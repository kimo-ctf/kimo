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

// Package integration runs the real controllers (SetupWithManager, actual
// watches/reconcile loops) against a real API server + etcd via envtest —
// not the fake client used by the internal/controller unit tests. envtest
// does not run kube-controller-manager or a kubelet, so Deployments never
// get real Pods on their own; tests that need a "Running" instance create
// the Pod object by hand to simulate what a kubelet would report.
package integration

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ctrl "sigs.k8s.io/controller-runtime"

	kimov1alpha1 "github.com/hermannchristopher/kimo/api/v1alpha1"
	"github.com/hermannchristopher/kimo/internal/controller"
	"github.com/hermannchristopher/kimo/internal/integrations"
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	// Real watch + reconcile round-trips through envtest's actual API
	// server take longer than Gomega's 1s default Eventually timeout.
	SetDefaultEventuallyTimeout(10 * time.Second)
	SetDefaultEventuallyPollingInterval(100 * time.Millisecond)
	RunSpecs(t, "integration suite")
}

var (
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
	cancel    context.CancelFunc
	backend   *recordingBackend
)

// recordingBackend is a Backend test double that records every Notify
// call, mirroring the one used in the internal/controller unit tests.
type recordingBackend struct {
	events []integrations.Event
}

func (b *recordingBackend) Name() string { return "recording" }
func (b *recordingBackend) Notify(_ context.Context, e integrations.Event) error {
	b.events = append(b.events, e)
	return nil
}
func (b *recordingBackend) Authenticate(_ *http.Request) (integrations.Principal, error) {
	return integrations.Principal{}, nil
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(kimov1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	Expect(err).NotTo(HaveOccurred())

	backend = &recordingBackend{}

	templateReconciler := &controller.ChallengeTemplateReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	Expect(templateReconciler.SetupWithManager(mgr)).To(Succeed())

	instanceReconciler := &controller.ChallengeInstanceReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Backend: backend,
	}
	Expect(instanceReconciler.SetupWithManager(mgr)).To(Succeed())

	networkFenceReconciler := &controller.NetworkFenceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	Expect(networkFenceReconciler.SetupWithManager(mgr)).To(Succeed())

	lifecycleReconciler := &controller.LifecycleReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), Backend: backend,
	}
	Expect(lifecycleReconciler.SetupWithManager(mgr)).To(Succeed())

	setReconciler := &controller.ChallengeSetReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}
	Expect(setReconciler.SetupWithManager(mgr)).To(Succeed())

	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	Expect(testEnv.Stop()).To(Succeed())
})
