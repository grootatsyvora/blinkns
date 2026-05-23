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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	blinknsv1alpha1 "github.com/grootatwork/blinkns/api/v1alpha1"
	"github.com/grootatwork/blinkns/pkg/ttl"
)

const (
	finalizerName = "blinkns.demo.io/cleanup"
	defaultOpNS   = "blinkns-system"
)

// BlinkNSReconciler reconciles BlinkNS objects.
type BlinkNSReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=blinkns.demo.io,resources=blinkns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=blinkns.demo.io,resources=blinkns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=blinkns.demo.io,resources=blinkns/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

func (r *BlinkNSReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	bns := &blinknsv1alpha1.BlinkNS{}
	if err := r.Get(ctx, req.NamespacedName, bns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// --- Deletion path ---
	if !bns.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, bns)
	}

	// --- Add finalizer on first reconcile ---
	if !controllerutil.ContainsFinalizer(bns, finalizerName) {
		controllerutil.AddFinalizer(bns, finalizerName)
		return ctrl.Result{}, r.Update(ctx, bns)
	}

	// --- Parse TTL (webhook should have already validated this) ---
	ttlDuration, err := ttl.ParseTTL(bns.Spec.TTL)
	if err != nil {
		logger.Error(err, "unparseable TTL — should have been caught by webhook")
		return ctrl.Result{}, nil
	}

	// --- Initialise status timestamps on first reconcile ---
	if bns.Status.CreatedAt == nil {
		now := metav1.Now()
		expiresAt := metav1.NewTime(now.Add(ttlDuration))
		warningAt := metav1.NewTime(expiresAt.Add(-ttlDuration / 10))

		bns.Status.CreatedAt = &now
		bns.Status.ExpiresAt = &expiresAt
		bns.Status.WarningAt = &warningAt
		bns.Status.Phase = blinknsv1alpha1.PhaseActive
		apimeta.SetStatusCondition(&bns.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "Initialising",
			Message:            "Namespace provisioning in progress",
			LastTransitionTime: now,
		})
		if err := r.Status().Update(ctx, bns); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// --- Ensure the namespace exists ---
	ns := &corev1.Namespace{}
	err = r.Get(ctx, types.NamespacedName{Name: bns.Name}, ns)
	if apierrors.IsNotFound(err) {
		if createErr := r.createNamespace(ctx, bns); createErr != nil {
			return ctrl.Result{}, createErr
		}
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Mark namespace as ready in conditions — only write if not already set
	existingCond := apimeta.FindStatusCondition(bns.Status.Conditions, "NamespaceCreated")
	if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
		apimeta.SetStatusCondition(&bns.Status.Conditions, metav1.Condition{
			Type:               "NamespaceCreated",
			Status:             metav1.ConditionTrue,
			Reason:             "Created",
			Message:            fmt.Sprintf("Namespace %s is active", bns.Name),
			LastTransitionTime: metav1.Now(),
		})
		apimeta.SetStatusCondition(&bns.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Active",
			Message:            fmt.Sprintf("Expires at %s", bns.Status.ExpiresAt.UTC().Format(time.RFC3339)),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, bns); err != nil {
			return ctrl.Result{}, err
		}
	}

	now := time.Now()
	expiresAt := bns.Status.ExpiresAt.Time
	warningAt := bns.Status.WarningAt.Time

	// --- TTL expired: delete the CR (finalizer will clean up the namespace) ---
	if now.After(expiresAt) {
		logger.Info("TTL expired, triggering deletion", "name", bns.Name)
		r.Recorder.Eventf(bns, corev1.EventTypeWarning, "TTLExpired",
			"TTL expired, deleting namespace %s", bns.Name)
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, bns))
	}

	// --- Warning notification: send once at warningAt ---
	if now.After(warningAt) && !bns.Status.NotificationSent {
		if notifyErr := r.sendWarning(ctx, bns); notifyErr != nil {
			logger.Error(notifyErr, "failed to send warning notification, will retry")
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		bns.Status.NotificationSent = true
		bns.Status.Phase = blinknsv1alpha1.PhaseExpiring
		apimeta.SetStatusCondition(&bns.Status.Conditions, metav1.Condition{
			Type:               "NotificationSent",
			Status:             metav1.ConditionTrue,
			Reason:             "Sent",
			Message:            "Warning notification delivered",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, bns); err != nil {
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(bns, corev1.EventTypeWarning, "NotificationSent",
			"Warning: namespace %s expires in %s", bns.Name,
			time.Until(expiresAt).Round(time.Minute))
		return ctrl.Result{RequeueAfter: time.Until(expiresAt)}, nil
	}

	// --- Requeue at the next interesting time ---
	if now.Before(warningAt) {
		return ctrl.Result{RequeueAfter: time.Until(warningAt)}, nil
	}
	return ctrl.Result{RequeueAfter: time.Until(expiresAt)}, nil
}

func (r *BlinkNSReconciler) createNamespace(ctx context.Context, bns *blinknsv1alpha1.BlinkNS) error {
	labels := map[string]string{"managed-by": "blinkns"}
	for k, v := range bns.Spec.Labels {
		labels[k] = v
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   bns.Name,
			Labels: labels,
		},
	}
	if err := r.Create(ctx, ns); err != nil {
		return err
	}
	r.Recorder.Eventf(bns, corev1.EventTypeNormal, "NamespaceCreated",
		"Namespace %s created, TTL: %s, expires at %s",
		bns.Name, bns.Spec.TTL, bns.Status.ExpiresAt.UTC().Format("2006-01-02T15:04Z"))
	return nil
}

func (r *BlinkNSReconciler) handleDeletion(ctx context.Context, bns *blinknsv1alpha1.BlinkNS) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(bns, finalizerName) {
		return ctrl.Result{}, nil
	}

	// Delete the actual Kubernetes namespace
	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: bns.Name}, ns)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err == nil {
		logger.Info("deleting namespace as part of BlinkNS cleanup", "namespace", bns.Name)
		if delErr := r.Delete(ctx, ns); delErr != nil && !apierrors.IsNotFound(delErr) {
			return ctrl.Result{}, delErr
		}
	}

	// Send terminated notification (best-effort)
	if notifyErr := r.sendTerminated(ctx, bns); notifyErr != nil {
		logger.Error(notifyErr, "failed to send terminated notification")
	}

	r.Recorder.Eventf(bns, corev1.EventTypeNormal, "NamespaceDeleted",
		"Namespace %s deleted successfully", bns.Name)

	// Mark phase as Terminated before removing the finalizer
	bns.Status.Phase = blinknsv1alpha1.PhaseTerminated
	if err := r.Status().Update(ctx, bns); err != nil {
		logger.Error(err, "failed to set phase Terminated")
		// non-fatal: proceed with finalizer removal
	}

	// Remove finalizer — allows K8s to delete the CR from etcd
	controllerutil.RemoveFinalizer(bns, finalizerName)
	return ctrl.Result{}, r.Update(ctx, bns)
}

func (r *BlinkNSReconciler) sendWarning(ctx context.Context, bns *blinknsv1alpha1.BlinkNS) error {
	if bns.Spec.Notifications == nil {
		return nil
	}
	url, err := r.getWebhookURL(ctx, bns)
	if err != nil {
		return err
	}
	return NewNotifier(bns.Spec.Notifications.WebhookType, url).
		SendWarning(ctx, bns.Name, bns.Status.ExpiresAt.Time)
}

func (r *BlinkNSReconciler) sendTerminated(ctx context.Context, bns *blinknsv1alpha1.BlinkNS) error {
	if bns.Spec.Notifications == nil {
		return nil
	}
	url, err := r.getWebhookURL(ctx, bns)
	if err != nil {
		return err
	}
	return NewNotifier(bns.Spec.Notifications.WebhookType, url).
		SendTerminated(ctx, bns.Name)
}

func (r *BlinkNSReconciler) getWebhookURL(ctx context.Context, bns *blinknsv1alpha1.BlinkNS) (string, error) {
	opNS := r.OperatorNamespace
	if opNS == "" {
		opNS = defaultOpNS
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: opNS,
		Name:      bns.Spec.Notifications.WebhookSecretRef,
	}, secret); err != nil {
		return "", fmt.Errorf("secret %q not found in namespace %q: %w",
			bns.Spec.Notifications.WebhookSecretRef, opNS, err)
	}
	webhookURL, ok := secret.Data["url"]
	if !ok {
		return "", fmt.Errorf("secret %q is missing required key \"url\"",
			bns.Spec.Notifications.WebhookSecretRef)
	}
	return string(webhookURL), nil
}

// SetupWithManager registers the controller with the manager.
func (r *BlinkNSReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&blinknsv1alpha1.BlinkNS{}).
		Complete(r)
}
