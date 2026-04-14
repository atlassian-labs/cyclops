# Node Cleanup Reconciler

## Problem

When a CycleNodeRequest (CNR) is deleted mid-lifecycle -- before reaching the
Successful or Healing phase -- the existing annotation cleanup in
`cleanupScaleDownDisabledAnnotations` (`pkg/controller/cyclenoderequest/transitioner/util.go`)
never runs. This leaves two stale annotations on nodes:

- `cluster-autoscaler.kubernetes.io/scale-down-disabled` -- prevents Cluster
  Autoscaler from scaling down the node
- `cyclops.atlassian.com/annotation-managed` -- Cyclops marker tracking that it
  added the above annotation

These orphaned annotations block Cluster Autoscaler indefinitely on affected
nodes.

## What was implemented

A new controller-runtime reconciler that watches Node objects and eventually
cleans up stale annotations left behind by deleted CNRs. It deliberately avoids
finalizers in favour of eventual consistency -- cleanup doesn't need to be
immediate.

### Files changed

| File | Change |
|------|--------|
| `pkg/controller/node/controller.go` | New reconciler (created) |
| `pkg/controller/node/controller_test.go` | Unit, predicate, and integration tests (created) |
| `cmd/manager/main.go` | Registers the new reconciler alongside the existing CNR and CNS controllers |
| `Makefile` | Version bump 1.10.4 → 1.10.5 |

### How it works

The reconciler uses the manager's shared Node cache (the same one the CNR
transitioner already populates via `ResourceManager.ListNodes`), so it adds zero
additional API traffic.

**Watch predicate:** Only nodes carrying the `cyclops.atlassian.com/annotation-managed`
annotation pass the predicate and enter the reconcile loop. All other nodes are
ignored at the watch level.

**Reconcile flow:**

1. Get the node from the cache.
2. Guard: if either annotation is missing, return early (nothing to clean up).
3. List all `NodeGroup` resources (cluster-scoped). Check whether the node's
   labels match at least one NodeGroup's `spec.nodeSelector`. If not, the node
   is not under Cyclops management -- skip it.
4. List all `CycleNodeRequest` resources in the configured namespace. For each
   non-terminal CNR (phase is not `Successful` or `Failed`), check whether its
   `spec.selector` matches the node's labels.
5. If the node matches an active CNR: requeue after 5 minutes and leave
   annotations in place.
6. If no active CNR covers the node: remove both annotations via JSON Patch
   (with retry-on-conflict).

The `namespace` field on the reconciler is used solely to scope the CNR list
query -- CycleNodeRequests are namespaced (defaults to `kube-system`).
NodeGroups are cluster-scoped and don't require a namespace.

### Design decisions and trade-offs

**No finalizer:** The cleanup is best-effort and eventually consistent. A 5-minute
requeue poll handles the gap between a CNR being deleted and the next reconcile.

**Shared cache:** Earlier iterations explored a dedicated Node cache with a
transform (to avoid caching full Node objects) and PartialObjectMetadata watches.
Both were discarded because the CNR transitioner's `ListNodes` already creates a
full Node informer in the manager's shared cache. A second informer would double
API traffic for no memory savings. The shared cache gives us access to all Node
fields -- including `spec.unschedulable` (cordon status) -- for free, which will
be needed if this reconciler is extended to uncordon nodes.

**NodeGroup gate:** The reconciler only acts on nodes selected by at least one
NodeGroup. This prevents it from touching nodes that happen to have the
annotations but are outside Cyclops management.

**Terminal phase definition:** `Successful` and `Failed` are terminal. `Healing`
is considered active because the existing cleanup logic runs during that phase.

## Extending this reconciler

The most likely next step is uncordoning nodes that were cordoned by a
deleted CNR. The full `corev1.Node` is available from the shared cache, so
`node.Spec.Unschedulable` can be checked directly. The `rawClient` on the
reconciler is already wired for JSON Patch operations via `k8s.UncordonNode`.

Other potential cleanup tasks:
- Removing the `cyclops.atlassian.com/terminate` label from nodes
- Removing the `cyclops.atlassian.com/nodegroup` annotation
- Deleting orphaned `CycleNodeStatus` objects

Each of these would follow the same pattern: check for the marker, verify no
active CNR covers the node, then remove.

## Tests

All tests are in `pkg/controller/node/controller_test.go`. Run with:

```
go test ./pkg/controller/node/ -v
```

**Unit tests (10 cases):** Table-driven, covering:
- Nodes with/without annotations and with/without matching NodeGroups
- Active CNRs with matching and non-matching selectors
- Terminal-only CNRs
- Single-annotation edge cases
- Node-not-found

**Predicate tests (7 cases):** Direct tests of the `hasCyclopsManagedAnnotation`
predicate for Create, Update, Delete, and Generic event types.

**Integration tests (2 cases):**
- Requeue-then-cleanup flow: first reconcile requeues while a CNR is active,
  CNR is deleted, second reconcile cleans up.
- Multiple nodes with mixed state: verifies only the correct node gets cleaned
  up while others are left untouched.

The tests construct the `Reconciler` directly with fake clients (controller-runtime
`fake.NewClientBuilder` and client-go `kubernetes/fake`), matching the existing
test patterns in the transitioner package. The `client` field on the Reconciler
struct was extracted from `mgr.GetClient()` specifically to enable this.
