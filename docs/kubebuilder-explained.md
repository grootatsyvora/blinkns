# Kubebuilder — What It Is, What It Does, and How BlinkNS Uses It

**Date:** 2026-05-23
**Purpose:** Tech talk reference — explains the kubebuilder toolchain and every file we wrote

---

## What Is Kubebuilder?

Kubebuilder is a framework for building Kubernetes operators in Go. It does two things:

1. **Scaffolds** the boilerplate project structure so you don't write 500 lines of plumbing from scratch
2. **Generates** Kubernetes manifests (CRD YAML, RBAC YAML, webhook config YAML) directly from annotations in your Go code

Think of it like this:

```
You write Go code with special comments  →  kubebuilder reads them  →  Kubernetes YAML is generated
```

The framework underneath is **controller-runtime** — the library that provides the reconciler loop, client, event recorder, webhook server, and everything else an operator needs to function.

---

## What Kubebuilder Generated (Don't Need to Touch)

When you run `kubebuilder init` and `kubebuilder create api`, it produces:

| Directory / File | What It Is |
|---|---|
| `config/` | All Kustomize YAML — CRD, RBAC, webhook manifests, cert-manager wiring |
| `Makefile` | Commands: `make build`, `make test`, `make generate`, `make manifests` |
| `go.mod` | Go module with all controller-runtime dependencies |
| `hack/boilerplate.go.txt` | License header template |
| `internal/controller/suite_test.go` | Envtest bootstrap (starts a real kube-apiserver for tests) |
| `PROJECT` | kubebuilder project metadata |

These files are **infrastructure** — they exist so the operator can run. You don't present them as "your code."

---

## What We Wrote (The 5 Teaching Files)

These are the files we actually authored:

```
api/v1alpha1/blinkns_types.go                    ← 1. The CRD schema
pkg/ttl/parser.go                                ← 2. TTL parsing
internal/controller/notifier.go                  ← 3. Slack/Discord HTTP client
internal/webhook/v1alpha1/blinkns_webhook.go     ← 4. Admission webhooks
internal/controller/blinkns_controller.go        ← 5. The reconciler loop
```

Plus the packaging layer:

```
cmd/main.go                    ← wires everything together at startup
Dockerfile                     ← builds a minimal container image
.github/workflows/ci.yaml      ← pushes the image to Docker Hub on every commit
helm/blinkns/                  ← deploys the whole operator with one helm install
```

---

## File 1: `api/v1alpha1/blinkns_types.go` — The CRD Schema

This file defines the Go structs that represent a `BlinkNS` object. Kubernetes needs to know the shape of your custom resource — this is where you define it.

```go
type BlinkNSSpec struct {
    TTL           string            // how long the namespace lives
    Labels        map[string]string // labels applied to the created namespace
    Notifications *NotificationSpec // optional Slack/Discord config
}

type BlinkNSStatus struct {
    Phase            BlinkNSPhase       // Pending | Active | Expiring | Terminated
    CreatedAt        *metav1.Time       // when namespace was provisioned
    ExpiresAt        *metav1.Time       // when it will be deleted
    WarningAt        *metav1.Time       // when the Slack warning fires
    NotificationSent bool               // has the warning been sent?
    Conditions       []metav1.Condition // standard K8s condition list
}
```

### The Kubebuilder Markers (Comments That Become YAML)

The lines starting with `// +kubebuilder:` directly above the `BlinkNS` struct are **not documentation** — they are code generation instructions:

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=bns
// +kubebuilder:printcolumn:name="TTL",type=string,JSONPath=`.spec.ttl`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=`.status.expiresAt`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type BlinkNS struct { ... }
```

| Marker | What It Does |
|---|---|
| `object:root=true` | Marks this as a top-level CRD kind (not just a nested struct) |
| `subresource:status` | Separates `.status` updates from `.spec` updates — the operator can update status without overwriting spec |
| `resource:scope=Cluster` | Makes BlinkNS cluster-scoped (no namespace prefix) — required because it provisions namespaces |
| `resource:shortName=bns` | Enables `kubectl get bns` as shorthand |
| `printcolumn` | Adds extra columns to `kubectl get bns` output (TTL, Phase, Expires, Age) |

### How to Run It

```bash
make generate    # regenerates api/v1alpha1/zz_generated.deepcopy.go
make manifests   # regenerates config/crd/bases/blinkns.demo.io_blinknses.yaml
```

The generated CRD YAML at `config/crd/bases/blinkns.demo.io_blinknses.yaml` is what Kubernetes actually installs to learn about the `BlinkNS` kind. You never write this by hand.

---

## File 2: `pkg/ttl/parser.go` — TTL Parsing

Go's standard library parses durations like `"10m"` and `"1h"` but not `"5d"` or `"1mo"`. This file adds those units.

```go
// Accepts: 10m, 1h, 12h, 189h, 1d, 20d, 1w, 1mo, 6mo, 1y
// Rejects: anything below 1m, anything above 1y, garbage input
func ParseTTL(s string) (time.Duration, error)
```

It uses a regex `^(\d+)(m|h|d|w|mo|y)$` to split the number from the unit, then multiplies:
- `d` = 24h, `w` = 7×24h, `mo` = 30×24h, `y` = 365×24h

Both the webhook (validation) and the reconciler (scheduling) call this same function.

---

## File 3: `internal/controller/notifier.go` — Slack/Discord HTTP Client

Sends a POST request to a Slack or Discord incoming webhook URL.

```go
// Warning: "⚠️ BlinkNS Warning: namespace `pr-42` expires in 4h48m (at 2026-05-24T10:00Z)"
func (n *Notifier) SendWarning(ctx context.Context, nsName string, expiresAt time.Time) error

// Terminated: "🗑️ BlinkNS: namespace `pr-42` has been deleted (TTL expired)"
func (n *Notifier) SendTerminated(ctx context.Context, nsName string) error
```

Slack expects `{"text": "..."}`, Discord expects `{"content": "..."}`. The `webhookType` field switches between them. The webhook URL is stored in a Kubernetes Secret and looked up at runtime — never hardcoded.

---

## File 4: `internal/webhook/v1alpha1/blinkns_webhook.go` — Admission Webhooks

Two webhooks intercept every `BlinkNS` create/update **before it is saved to etcd**. This is Kubernetes' admission control layer.

### Mutating Webhook (Defaulting) — fires on CREATE and UPDATE

```go
func (d *BlinkNSCustomDefaulter) Default(_ context.Context, obj *blinknsv1alpha1.BlinkNS) error {
    if obj.Spec.TTL == "" {
        obj.Spec.TTL = "24h"        // default TTL if not specified
    }
    obj.Spec.Labels["managed-by"] = "blinkns"  // always stamp this label
    return nil
}
```

The kubebuilder marker that registers this webhook:
```go
// +kubebuilder:webhook:path=/mutate-blinkns-demo-io-v1alpha1-blinkns,
//   mutating=true,failurePolicy=fail,...
```

### Validating Webhook — fires on CREATE and UPDATE

Rejects the request (returns an error) if:
- Name is a reserved namespace: `default`, `kube-system`, `kube-public`, `kube-node-lease`
- TTL is below `1m` (too short) or above `1y` (not ephemeral)
- `spec.ttl` is being changed on UPDATE — it's **immutable** after creation
- `webhookType` is not `slack` or `discord`

```go
// +kubebuilder:webhook:path=/validate-blinkns-demo-io-v1alpha1-blinkns,
//   mutating=false,failurePolicy=fail,...
```

### How kubebuilder wires these up

Running `make manifests` reads those `// +kubebuilder:webhook:` markers and generates `config/webhook/manifests.yaml` — the `MutatingWebhookConfiguration` and `ValidatingWebhookConfiguration` objects that tell kube-apiserver "send these requests to the operator's webhook server before saving them."

---

## File 5: `internal/controller/blinkns_controller.go` — The Reconciler (Core)

This is the heart of any Kubernetes operator. The `Reconcile` function is called by controller-runtime every time a `BlinkNS` object is created, updated, or deleted — and also whenever the operator schedules a requeue.

### The State Machine

```
Reconcile() called
    │
    ├─ DeletionTimestamp set? → handleDeletion()
    │       delete namespace
    │       send terminated notification
    │       remove finalizer → K8s completes deletion
    │
    ├─ No finalizer? → add "blinkns.demo.io/cleanup" finalizer, requeue
    │
    ├─ CreatedAt == nil? → first reconcile
    │       calculate expiresAt = now + ttl
    │       calculate warningAt = expiresAt - ttl/10
    │       set phase = Active
    │       requeue immediately
    │
    ├─ Namespace missing? → create it with labels, emit NamespaceCreated event
    │
    ├─ now > expiresAt? → delete the CR
    │       (this triggers DeletionTimestamp → finalizer handles cleanup)
    │
    ├─ now > warningAt AND !notificationSent?
    │       send Slack/Discord warning
    │       set notificationSent = true, phase = Expiring
    │       emit NotificationSent event
    │       requeue at expiresAt
    │
    └─ else → requeue at warningAt (or expiresAt if already past it)
```

### The Finalizer — Why It Exists

A finalizer is a string added to `metadata.finalizers`. Kubernetes will not delete an object from etcd until all finalizers are removed.

```
Without finalizer:  kubectl delete blinkns pr-42
                    → CR instantly gone from etcd
                    → namespace pr-42 lives forever (orphaned)

With finalizer:     kubectl delete blinkns pr-42
                    → K8s sets DeletionTimestamp (marks for deletion)
                    → operator's Reconcile() is called
                    → operator deletes namespace, sends notification
                    → operator removes finalizer
                    → K8s removes CR from etcd
```

### Efficient Requeuing

The reconciler never polls. Instead of waking up every 30 seconds to check "is it time yet?", it tells controller-runtime exactly when to wake it up:

```go
// Sleep until warningAt, then wake and send the warning
return ctrl.Result{RequeueAfter: time.Until(warningAt)}, nil

// Sleep until expiresAt, then wake and delete
return ctrl.Result{RequeueAfter: time.Until(expiresAt)}, nil
```

### RBAC Markers

```go
// +kubebuilder:rbac:groups=blinkns.demo.io,resources=blinknses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
```

`make manifests` reads these and generates `config/rbac/role.yaml` — the `ClusterRole` that grants the operator exactly the permissions it needs, nothing more.

---

## How the Markers Flow Into Kubernetes

```
Your Go file with markers
        │
        ▼
  make generate                       →  zz_generated.deepcopy.go  (DeepCopy methods)
  make manifests                      →  config/crd/bases/*.yaml    (CRD definition)
                                      →  config/rbac/role.yaml      (ClusterRole)
                                      →  config/webhook/manifests.yaml (Webhook configs)
        │
        ▼
  helm/blinkns/crds/blinkns.yaml      (copied from config/crd/bases/)
  helm/blinkns/templates/             (references the same webhook paths)
        │
        ▼
  helm install blinkns ./helm/blinkns
        │
        ▼
  Kubernetes cluster knows about BlinkNS,
  has the operator running,
  webhooks registered,
  RBAC in place
```

---

## The `make` Commands You'll Use

| Command | What It Does |
|---|---|
| `make generate` | Regenerates `zz_generated.deepcopy.go` from type structs |
| `make manifests` | Regenerates CRD YAML, RBAC YAML, webhook YAML from markers |
| `make build` | Compiles the operator binary to `bin/manager` |
| `make test` | Runs all tests (unit + envtest integration) |
| `make docker-build IMG=...` | Builds the Docker image |

**Rule of thumb:** any time you change `blinkns_types.go` or add/remove `// +kubebuilder:` markers, run `make generate manifests` before anything else.

---

## The Demo Flow (What the Audience Sees)

```bash
# 1. Install the operator + create a 5-minute namespace
helm install blinkns ./helm/blinkns \
  --set notifications.webhookUrl=$SLACK_URL \
  --set namespaces[0].name=demo-ns \
  --set namespaces[0].ttl=5m

# 2. Watch it appear
kubectl get bns          # shows: demo-ns | 5m | Active | <expiresAt>
kubectl get ns demo-ns   # namespace is live

# 3. Show the status conditions and events
kubectl describe bns demo-ns

# 4. At 4m30s — Slack message arrives
# 5. At 5m    — namespace is gone
kubectl get ns demo-ns   # Error: not found
kubectl get bns          # empty — CR is gone too
```
