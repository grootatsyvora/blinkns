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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BlinkNSPhase is the lifecycle phase of a BlinkNS resource.
type BlinkNSPhase string

const (
	PhasePending    BlinkNSPhase = "Pending"
	PhaseActive     BlinkNSPhase = "Active"
	PhaseExpiring   BlinkNSPhase = "Expiring"
	PhaseTerminated BlinkNSPhase = "Terminated"
)

// NotificationSpec configures Slack or Discord webhook alerts.
type NotificationSpec struct {
	// WebhookType is "slack" or "discord".
	// +kubebuilder:validation:Enum=slack;discord
	WebhookType string `json:"webhookType"`

	// WebhookSecretRef is the name of a Secret in the blinkns-system namespace.
	// The Secret must have a key named "url" containing the webhook URL.
	WebhookSecretRef string `json:"webhookSecretRef"`
}

// BlinkNSSpec defines the desired state of a BlinkNS resource.
type BlinkNSSpec struct {
	// TTL is how long the namespace lives before being deleted.
	// Accepts any positive integer with a unit: m, h, d, w, mo, y.
	// Examples: "30m", "12h", "189h", "20d", "1w", "6mo", "1y".
	// Minimum: 1m. Maximum: 1y (8760h). Immutable after creation.
	// +kubebuilder:validation:Required
	TTL string `json:"ttl"`

	// Labels are applied to the created namespace.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Notifications configures a warning alert sent at ttl/10 before expiry.
	// +optional
	Notifications *NotificationSpec `json:"notifications,omitempty"`
}

// BlinkNSStatus defines the observed state of a BlinkNS resource.
type BlinkNSStatus struct {
	// Phase is the current lifecycle state.
	// +optional
	Phase BlinkNSPhase `json:"phase,omitempty"`

	// CreatedAt is when the namespace was provisioned.
	// +optional
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// ExpiresAt is when the namespace will be deleted.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// WarningAt is when the pre-expiry notification fires (expiresAt - ttl/10).
	// +optional
	WarningAt *metav1.Time `json:"warningAt,omitempty"`

	// NotificationSent is true once the warning notification has been delivered.
	// +optional
	NotificationSent bool `json:"notificationSent,omitempty"`

	// Conditions are the latest observations of the BlinkNS state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=bns
// +kubebuilder:printcolumn:name="TTL",type=string,JSONPath=`.spec.ttl`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=`.status.expiresAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BlinkNS provisions a Kubernetes namespace that is automatically deleted when its TTL expires.
type BlinkNS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BlinkNSSpec   `json:"spec,omitempty"`
	Status BlinkNSStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BlinkNSList contains a list of BlinkNS.
type BlinkNSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BlinkNS `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BlinkNS{}, &BlinkNSList{})
}
