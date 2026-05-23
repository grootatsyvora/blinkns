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

package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	blinknsv1alpha1 "github.com/grootatwork/blinkns/api/v1alpha1"
	"github.com/grootatwork/blinkns/pkg/ttl"
)

var blinknslog = logf.Log.WithName("blinkns-resource")

var reservedNames = map[string]bool{
	"default":         true,
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
	"blinkns-system":  true,
}

// SetupBlinkNSWebhookWithManager registers the webhook for BlinkNS in the manager.
func SetupBlinkNSWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &blinknsv1alpha1.BlinkNS{}).
		WithValidator(&BlinkNSCustomValidator{}).
		WithDefaulter(&BlinkNSCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-blinkns-demo-io-v1alpha1-blinkns,mutating=true,failurePolicy=fail,sideEffects=None,groups=blinkns.demo.io,resources=blinkns,verbs=create;update,versions=v1alpha1,name=mblinkns-v1alpha1.kb.io,admissionReviewVersions=v1

// BlinkNSCustomDefaulter sets default values on BlinkNS resources.
type BlinkNSCustomDefaulter struct{}

// Default fills in missing fields with sensible defaults. Fires on CREATE.
func (d *BlinkNSCustomDefaulter) Default(_ context.Context, obj *blinknsv1alpha1.BlinkNS) error {
	blinknslog.Info("Defaulting BlinkNS", "name", obj.GetName())

	if obj.Spec.TTL == "" {
		obj.Spec.TTL = "24h"
	}
	if obj.Spec.Labels == nil {
		obj.Spec.Labels = make(map[string]string)
	}
	obj.Spec.Labels["managed-by"] = "blinkns"

	return nil
}

// +kubebuilder:webhook:path=/validate-blinkns-demo-io-v1alpha1-blinkns,mutating=false,failurePolicy=fail,sideEffects=None,groups=blinkns.demo.io,resources=blinkns,verbs=create;update,versions=v1alpha1,name=vblinkns-v1alpha1.kb.io,admissionReviewVersions=v1

// BlinkNSCustomValidator validates BlinkNS resources on create and update.
type BlinkNSCustomValidator struct{}

// ValidateCreate validates a new BlinkNS resource.
func (v *BlinkNSCustomValidator) ValidateCreate(_ context.Context, obj *blinknsv1alpha1.BlinkNS) (admission.Warnings, error) {
	blinknslog.Info("Validating BlinkNS create", "name", obj.GetName())
	return nil, validate(obj)
}

// ValidateUpdate validates an update. spec.ttl is immutable after creation.
func (v *BlinkNSCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *blinknsv1alpha1.BlinkNS) (admission.Warnings, error) {
	blinknslog.Info("Validating BlinkNS update", "name", newObj.GetName())
	if newObj.Spec.TTL != oldObj.Spec.TTL {
		return nil, fmt.Errorf("spec.ttl is immutable: cannot change %q to %q after creation", oldObj.Spec.TTL, newObj.Spec.TTL)
	}
	return nil, validate(newObj)
}

// ValidateDelete is always allowed.
func (v *BlinkNSCustomValidator) ValidateDelete(_ context.Context, obj *blinknsv1alpha1.BlinkNS) (admission.Warnings, error) {
	return nil, nil
}

func validate(obj *blinknsv1alpha1.BlinkNS) error {
	if reservedNames[obj.Name] {
		return fmt.Errorf("name %q is reserved — choose a different name", obj.Name)
	}
	if _, err := ttl.ParseTTL(obj.Spec.TTL); err != nil {
		return fmt.Errorf("spec.ttl is invalid: %w", err)
	}
	if obj.Spec.Notifications != nil {
		if obj.Spec.Notifications.WebhookType != "slack" && obj.Spec.Notifications.WebhookType != "discord" {
			return fmt.Errorf("spec.notifications.webhookType must be \"slack\" or \"discord\", got %q", obj.Spec.Notifications.WebhookType)
		}
		if obj.Spec.Notifications.WebhookSecretRef == "" {
			return fmt.Errorf("spec.notifications.webhookSecretRef is required when notifications is set")
		}
	}
	return nil
}
