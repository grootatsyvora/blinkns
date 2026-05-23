package controller_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grootatwork/blinkns/internal/controller"
)

func TestNotifier_SendWarning_Slack(t *testing.T) {
	var received map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := controller.NewNotifier("slack", srv.URL)
	expiresAt := time.Now().Add(2 * time.Hour)
	err := n.SendWarning(context.Background(), "pr-42-backend", expiresAt)

	if err != nil {
		t.Fatalf("SendWarning returned error: %v", err)
	}
	if received["text"] == "" {
		t.Error("expected 'text' field in Slack payload, got empty string")
	}
}

func TestNotifier_SendWarning_Discord(t *testing.T) {
	var received map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := controller.NewNotifier("discord", srv.URL)
	err := n.SendWarning(context.Background(), "demo-ns", time.Now().Add(time.Hour))

	if err != nil {
		t.Fatalf("SendWarning returned error: %v", err)
	}
	if received["content"] == "" {
		t.Error("expected 'content' field in Discord payload, got empty string")
	}
}

func TestNotifier_SendTerminated(t *testing.T) {
	var received map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := controller.NewNotifier("slack", srv.URL)
	err := n.SendTerminated(context.Background(), "demo-ns")

	if err != nil {
		t.Fatalf("SendTerminated returned error: %v", err)
	}
	if received["text"] == "" {
		t.Error("expected 'text' field in Slack payload")
	}
}

func TestNotifier_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := controller.NewNotifier("slack", srv.URL)
	err := n.SendWarning(context.Background(), "demo-ns", time.Now().Add(time.Hour))

	if err == nil {
		t.Error("expected error for HTTP 500, got nil")
	}
}
