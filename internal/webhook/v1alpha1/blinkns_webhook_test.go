package v1alpha1_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	blinknsv1alpha1 "github.com/grootatwork/blinkns/api/v1alpha1"
	webhookv1alpha1 "github.com/grootatwork/blinkns/internal/webhook/v1alpha1"
)

func newBlinkNS(name, ttlStr string) *blinknsv1alpha1.BlinkNS {
	return &blinknsv1alpha1.BlinkNS{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       blinknsv1alpha1.BlinkNSSpec{TTL: ttlStr},
	}
}

func TestDefault_SetsTTL(t *testing.T) {
	d := &webhookv1alpha1.BlinkNSCustomDefaulter{}
	b := newBlinkNS("test-ns", "")
	if err := d.Default(context.Background(), b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Spec.TTL != "24h" {
		t.Errorf("expected default TTL 24h, got %q", b.Spec.TTL)
	}
}

func TestDefault_SetsLabel(t *testing.T) {
	d := &webhookv1alpha1.BlinkNSCustomDefaulter{}
	b := newBlinkNS("test-ns", "1h")
	if err := d.Default(context.Background(), b); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Spec.Labels["managed-by"] != "blinkns" {
		t.Errorf("expected managed-by=blinkns, got %q", b.Spec.Labels["managed-by"])
	}
}

func TestValidateCreate_RejectsReservedName(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	for _, name := range []string{"default", "kube-system", "kube-public", "kube-node-lease"} {
		b := newBlinkNS(name, "1h")
		_, err := v.ValidateCreate(context.Background(), b)
		if err == nil {
			t.Errorf("expected error for reserved name %q, got nil", name)
		}
	}
}

func TestValidateCreate_RejectsTTLTooShort(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	b := newBlinkNS("my-ns", "0m")
	_, err := v.ValidateCreate(context.Background(), b)
	if err == nil {
		t.Error("expected error for TTL 0m, got nil")
	}
}

func TestValidateCreate_RejectsTTLTooLong(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	b := newBlinkNS("my-ns", "9000d")
	_, err := v.ValidateCreate(context.Background(), b)
	if err == nil {
		t.Error("expected error for TTL 9000d, got nil")
	}
}

func TestValidateCreate_AcceptsValidCR(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	b := newBlinkNS("pr-42-backend", "48h")
	_, err := v.ValidateCreate(context.Background(), b)
	if err != nil {
		t.Errorf("unexpected error for valid CR: %v", err)
	}
}

func TestValidateUpdate_RejectsTTLChange(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	old := newBlinkNS("my-ns", "24h")
	updated := newBlinkNS("my-ns", "48h")
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err == nil {
		t.Error("expected error for TTL change, got nil")
	}
}

func TestValidateUpdate_AllowsOtherChanges(t *testing.T) {
	v := &webhookv1alpha1.BlinkNSCustomValidator{}
	old := newBlinkNS("my-ns", "24h")
	updated := newBlinkNS("my-ns", "24h")
	updated.Spec.Labels = map[string]string{"env": "staging"}
	_, err := v.ValidateUpdate(context.Background(), old, updated)
	if err != nil {
		t.Errorf("unexpected error for non-TTL update: %v", err)
	}
}
