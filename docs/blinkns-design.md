# BlinkNS Operator — Design Spec

**Date:** 2026-05-22  
**Author:** Deependra Parmar  
**Purpose:** Tech talk — "Decoding K8s Operators"

---

## Overview

BlinkNS is a Kubernetes operator that provisions namespaces with a TTL (time-to-live). When the TTL expires, the namespace and all its contents are automatically deleted. A warning notification is sent to Slack or Discord at `ttl/10` time before expiry.

**The problem it solves:**
> Teams create namespaces for PRs, integration tests, and demos. Nobody cleans them up. Clusters accumulate zombie namespaces eating resources and creating security surface area.

**One CR, full lifecycle managed:**
```
Created → Namespace provisioned → Warning sent at ttl/10 → TTL expires → Namespace deleted → CR removed
```

---

## Naming

| Layer | Value |
|---|---|
| Operator name | `blinkns` |
| CRD kind | `BlinkNS` |
| API group | `blinkns.demo.io` |
| API version | `v1alpha1` |
| Go module | `github.com/grootatwork/blinkns` |
| Docker image | `grootatsyvora/blinkns` |
| Helm chart | `blinkns` |

---

## CRD: `BlinkNS`

### Spec

```yaml
apiVersion: blinkns.demo.io/v1alpha1
kind: BlinkNS
metadata:
  name: pr-42-backend
spec:
  ttl: "48h"                        # required — supports: 10m, 1h, 12h, 1d, 1w, 1mo, 1y
  labels:                           # optional — applied to the created namespace
    team: backend
    pr: "42"
  notifications:
    webhookType: slack               # slack | discord
    webhookSecretRef: slack-secret   # name of K8s Secret containing webhook URL key
```

### Status

```yaml
status:
  phase: Active                      # Pending | Active | Expiring | Terminated
  createdAt: "2026-05-22T10:00:00Z"
  expiresAt: "2026-05-24T10:00:00Z"
  warningAt: "2026-05-24T05:21:36Z"  # expiresAt - (ttl / 10)
  notificationSent: false
  conditions:
    - type: NamespaceCreated
      status: "True"
      lastTransitionTime: "2026-05-22T10:00:00Z"
    - type: NotificationSent
      status: "False"
      lastTransitionTime: "2026-05-22T10:00:00Z"
    - type: Ready
      status: "True"
      lastTransitionTime: "2026-05-22T10:00:00Z"
```

### TTL format

Custom parser supporting any positive integer with a unit suffix. Go's `time.Duration` handles `m` and `h` natively; `d`, `w`, `mo`, `y` are converted before parsing.

| Unit | Meaning | Examples |
|---|---|---|
| `m` | minutes | `10m`, `90m` |
| `h` | hours | `1h`, `12h`, `15h`, `189h` |
| `d` | days | `1d`, `20d` |
| `w` | weeks | `1w`, `3w` |
| `mo` | months (30 days) | `1mo`, `6mo` |
| `y` | years (365 days) | `1y` |

Any combination works: `189h`, `20d`, `15h`, `6mo` are all valid. Maximum allowed is `8760h` (1 year = 365 days). Minimum is `1m`.

---

## Webhooks

### Mutating (Defaulting) Webhook

Fires on `CREATE` of a `BlinkNS` resource. Sets defaults if fields are omitted:

| Field | Default |
|---|---|
| `spec.ttl` | `"24h"` |
| `spec.labels["managed-by"]` | `"blinkns"` |

### Validating Webhook

Fires on `CREATE` and `UPDATE`. Rejects if:

- `metadata.name` matches a reserved namespace: `default`, `kube-system`, `kube-public`, `kube-node-lease`
- `spec.ttl` parses to less than `1m` (too short to be useful)
- `spec.ttl` parses to more than `1y` (not ephemeral)
- `spec.notifications.webhookType` is not `slack` or `discord`
- `spec.notifications.webhookSecretRef` is set but the referenced Secret does not exist
- On `UPDATE`: `spec.ttl` has changed (immutable after creation — you can't extend a blink)

---

## Reconciler Logic

### State Machine

```
CR Created
    ↓  add finalizer: blinkns.demo.io/cleanup
    ↓  create Namespace with spec.labels
    ↓  set status.phase = Active
    ↓  set status.createdAt, expiresAt, warningAt
    ↓  requeue at warningAt

warningAt reached
    ↓  send Slack/Discord warning notification
    ↓  set status.notificationSent = true
    ↓  set status.phase = Expiring
    ↓  emit K8s Event: "TTL expiring soon, namespace will be deleted"
    ↓  requeue at expiresAt

expiresAt reached
    ↓  delete the BlinkNS CR (triggers finalizer)

Finalizer runs on deletion
    ↓  delete the actual K8s Namespace
    ↓  send Slack/Discord "terminated" notification
    ↓  emit K8s Event: "Namespace deleted, TTL expired"
    ↓  remove finalizer → CR is deleted from etcd
```

### Requeue Strategy

The reconciler uses `ctrl.Result{RequeueAfter: duration}` to schedule itself efficiently:

- After creating namespace → requeue at `warningAt`
- After sending warning → requeue at `expiresAt`
- No polling loop — the reconciler only wakes when needed

---

## Notifications

### Slack payload

```json
{
  "text": "⚠️ BlinkNS Warning: namespace `pr-42-backend` expires in 4h48m (at 2026-05-24T10:00Z)"
}
```

### Discord payload

```json
{
  "content": "⚠️ BlinkNS Warning: namespace `pr-42-backend` expires in 4h48m (at 2026-05-24T10:00Z)"
}
```

### Secret format

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: slack-secret
type: Opaque
stringData:
  url: "https://hooks.slack.com/services/..."
```

---

## Finalizer

Name: `blinkns.demo.io/cleanup`

Added to every `BlinkNS` CR on first reconcile. Prevents K8s from removing the CR from etcd until the operator has:
1. Deleted the actual namespace
2. Sent the "terminated" notification

**Teaching moment:** Without the finalizer, `kubectl delete blinkns pr-42-backend` would instantly remove the CR but leave the namespace orphaned forever.

---

## K8s Events

Emitted via the Kubernetes event recorder. Visible with `kubectl describe blinkns <name>`.

| Reason | Type | Message |
|---|---|---|
| `NamespaceCreated` | Normal | Namespace pr-42-backend created, TTL: 48h, expires at 2026-05-24T10:00Z |
| `NotificationSent` | Warning | TTL expiring in 4h48m — warning sent to Slack |
| `TTLExpired` | Warning | TTL expired, deleting namespace pr-42-backend |
| `NamespaceDeleted` | Normal | Namespace pr-42-backend deleted successfully |

---

## RBAC

The operator needs a `ClusterRole` (namespaces are cluster-scoped):

```yaml
rules:
  - apiGroups: [""]
    resources: ["namespaces"]
    verbs: ["get", "list", "watch", "create", "delete"]
  - apiGroups: ["blinkns.demo.io"]
    resources: ["blinkns", "blinkns/status", "blinkns/finalizers"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
```

---

## Project Structure

```
blinkns/                                     ← current directory
├── cmd/
│   └── main.go                              # entry point, leader election setup
├── api/
│   └── v1alpha1/
│       ├── blinkns_types.go                 # CRD Go structs
│       ├── blinkns_webhook.go               # mutating + validating logic
│       └── zz_generated.deepcopy.go         # auto-generated by controller-gen
├── internal/
│   └── controller/
│       ├── blinkns_controller.go            # reconciler loop
│       └── notifier.go                      # Slack/Discord HTTP calls
├── config/                                  # kubebuilder-generated manifests
│   ├── crd/
│   ├── rbac/
│   └── webhook/
├── helm/
│   └── blinkns/
│       ├── Chart.yaml
│       ├── values.yaml                      # namespaces list goes here
│       └── templates/
│           ├── deployment.yaml
│           ├── clusterrole.yaml
│           ├── clusterrolebinding.yaml
│           ├── crd.yaml
│           ├── webhook.yaml
│           ├── secret.yaml
│           └── blinkns-list.yaml            # renders one BlinkNS CR per values entry
├── Dockerfile
├── README.md
└── .github/
    └── workflows/
        └── ci.yaml                              # build + push to Docker Hub
```

---

## Helm `values.yaml` shape

```yaml
image:
  repository: grootatsyvora/blinkns
  tag: latest

notifications:
  webhookType: slack
  webhookUrl: ""              # set at install time: --set notifications.webhookUrl=...

namespaces:
  - name: pr-42-backend
    ttl: "48h"
    labels:
      team: backend
  - name: integration-tests
    ttl: "2h"
  - name: demo-env
    ttl: "1d"
    labels:
      purpose: demo
```

---

## Image Registry & CI

### Docker Hub
- Image: `grootatsyvora/blinkns`
- Tags: `latest` (main branch), `v0.1.0` (git tags)

### GitHub Actions CI

Triggers:
- Push to `main` → build and push `grootatsyvora/blinkns:latest`
- Push a tag `v*` → build and push `grootatsyvora/blinkns:<tag>`

Required GitHub secrets:
- `DOCKERHUB_USERNAME` = `grootatsyvora`
- `DOCKERHUB_TOKEN` = Docker Hub access token (Settings → Security → New Access Token)

Workflow file: `.github/workflows/ci.yaml`

```yaml
name: ci
on:
  push:
    branches: [main]
    tags: ["v*"]
jobs:
  build-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
      - uses: docker/metadata-action@v5
        id: meta
        with:
          images: grootatsyvora/blinkns
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}
      - uses: docker/build-push-action@v5
        with:
          push: true
          tags: ${{ steps.meta.outputs.tags }}
```

### Helm Registry

- Registry: GitHub Container Registry (GHCR) via GitHub Packages (OCI)
- Install: `helm install blinkns oci://ghcr.io/grootatwork/blinkns --version 0.1.0`

---

## Live Demo Script (30–45 min)

1. **Show the problem** — open cluster with leftover namespaces
2. **Install operator** — `helm install blinkns ./helm/blinkns --set notifications.webhookUrl=$SLACK_URL --set namespaces[0].name=demo,namespaces[0].ttl=5m`
3. **Watch namespace appear** — `kubectl get ns` + `kubectl get blinkns`
4. **Show status** — `kubectl describe blinkns demo` → conditions + events
5. **Demo validating webhook** — apply CR with `ttl=10s` → rejected with clear error
6. **Demo mutating webhook** — apply CR without TTL → show it gets defaulted to `24h`
7. **Demo immutability** — try to patch `spec.ttl` → rejected
8. **Wait for warning** — 5m TTL → alert at 30s → Slack message appears
9. **Watch expiry** — namespace deletes itself, events show full cleanup sequence
10. **Demo finalizer** — `kubectl delete blinkns demo` mid-life → finalizer intercepts, cleans namespace, then completes

---

## Concepts Covered

| K8s Operator Concept | Where it appears |
|---|---|
| CRD | `BlinkNS` resource definition |
| Controller / Reconciler loop | `blinkns_controller.go` |
| Status subresource | `status.phase`, `status.conditions` |
| Mutating (defaulting) webhook | Default TTL, managed-by label |
| Validating webhook | Reserved names, TTL bounds, immutability |
| Finalizer | Cleanup before deletion |
| Owner references | N/A (Namespace is cluster-scoped) |
| K8s Events | Timeline visible in `kubectl describe` |
| Leader election | Multi-replica safe operation |
| RBAC / ClusterRole | Cluster-scoped namespace access |
