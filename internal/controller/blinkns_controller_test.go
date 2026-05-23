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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	blinknsv1alpha1 "github.com/grootatwork/blinkns/api/v1alpha1"
)

var _ = Describe("BlinkNS Controller", func() {
	const timeout = 15 * time.Second
	const interval = 250 * time.Millisecond

	ctx := context.Background()

	Context("When a BlinkNS is created", func() {
		It("should add a finalizer", func() {
			bns := &blinknsv1alpha1.BlinkNS{
				ObjectMeta: metav1.ObjectMeta{Name: "test-finalizer"},
				Spec:       blinknsv1alpha1.BlinkNSSpec{TTL: "1h"},
			}
			Expect(k8sClient.Create(ctx, bns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bns)
			})

			Eventually(func() bool {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-finalizer"}, bns); err != nil {
					return false
				}
				for _, f := range bns.Finalizers {
					if f == "blinkns.demo.io/cleanup" {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should create the namespace", func() {
			bns := &blinknsv1alpha1.BlinkNS{
				ObjectMeta: metav1.ObjectMeta{Name: "test-ns-create"},
				Spec:       blinknsv1alpha1.BlinkNSSpec{TTL: "1h"},
			}
			Expect(k8sClient.Create(ctx, bns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bns)
			})

			ns := &corev1.Namespace{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-ns-create"}, ns)
			}, timeout, interval).Should(Succeed())

			Expect(ns.Labels["managed-by"]).To(Equal("blinkns"))
		})

		It("should set status.phase to Active", func() {
			bns := &blinknsv1alpha1.BlinkNS{
				ObjectMeta: metav1.ObjectMeta{Name: "test-status"},
				Spec:       blinknsv1alpha1.BlinkNSSpec{TTL: "1h"},
			}
			Expect(k8sClient.Create(ctx, bns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bns)
			})

			Eventually(func() blinknsv1alpha1.BlinkNSPhase {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-status"}, bns); err != nil {
					return ""
				}
				return bns.Status.Phase
			}, timeout, interval).Should(Equal(blinknsv1alpha1.PhaseActive))

			Expect(bns.Status.ExpiresAt).NotTo(BeNil())
			Expect(bns.Status.WarningAt).NotTo(BeNil())
		})

		It("should delete the namespace when the CR is deleted", func() {
			bns := &blinknsv1alpha1.BlinkNS{
				ObjectMeta: metav1.ObjectMeta{Name: "test-deletion"},
				Spec:       blinknsv1alpha1.BlinkNSSpec{TTL: "1h"},
			}
			Expect(k8sClient.Create(ctx, bns)).To(Succeed())

			ns := &corev1.Namespace{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-deletion"}, ns)
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, bns)).To(Succeed())

			// After CR deletion the reconciler removes the finalizer; wait for the CR to be gone.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-deletion"}, bns)
				return apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue(), "BlinkNS CR should be deleted")

			// The namespace is deleted by the reconciler. In envtest namespaces may linger
			// in Terminating state, so use Eventually and accept either NotFound or Terminating.
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-deletion"}, ns)
				if apierrors.IsNotFound(err) {
					return true
				}
				return err == nil && ns.DeletionTimestamp != nil
			}, timeout, interval).Should(BeTrue(), "namespace should be deleted or terminating")
		})
	})

	Context("When custom labels are specified", func() {
		It("should apply them to the namespace", func() {
			bns := &blinknsv1alpha1.BlinkNS{
				ObjectMeta: metav1.ObjectMeta{Name: "test-labels"},
				Spec: blinknsv1alpha1.BlinkNSSpec{
					TTL: "1h",
					Labels: map[string]string{
						"team": "backend",
						"env":  "test",
					},
				},
			}
			Expect(k8sClient.Create(ctx, bns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, bns)
			})

			ns := &corev1.Namespace{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "test-labels"}, ns)
			}, timeout, interval).Should(Succeed())

			Expect(ns.Labels["team"]).To(Equal("backend"))
			Expect(ns.Labels["env"]).To(Equal("test"))
			Expect(ns.Labels["managed-by"]).To(Equal("blinkns"))

			Expect(k8sClient.Delete(ctx, bns)).To(Succeed())
		})
	})
})
