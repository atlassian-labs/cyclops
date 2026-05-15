# Node Controller

The node controller is a controller-runtime reconciler for Kubernetes `Node` objects. Its current responsibility is to remove stale Cyclops-managed Cluster Autoscaler annotations that can be left behind when a `CycleNodeRequest` is deleted before normal CNR cleanup runs.

## Why it exists

Cyclops adds these annotations to replacement nodes during cycling:

- `cluster-autoscaler.kubernetes.io/scale-down-disabled=true`
- `cyclops.atlassian.com/annotation-managed=true`

The first annotation prevents Cluster Autoscaler from removing replacement nodes before the old nodes have drained and terminated. The second annotation records that Cyclops added the protection, so Cyclops does not remove annotations that were pre-existing on a node.

If a CNR disappears mid-cycle, the normal CNR transitioner cleanup may never run. The node controller acts as an eventual-consistency backstop so those nodes are not protected from scale-down forever.

## How it works

The controller watches `Node` objects using the manager's shared cache. A predicate only enqueues nodes with the Cyclops marker annotation, so ordinary node changes do not enter the reconcile loop.

For each matching node, the controller:

1. Confirms both the Cyclops marker and scale-down-disabled annotations are still present.
2. Confirms the node is selected by at least one `NodeGroup`.
3. Checks whether any non-terminal CNR in the configured namespace still selects the node.
4. Requeues after the configured interval if an active CNR still covers the node.
5. Removes both annotations if no active CNR covers the node.

`Successful` and `Failed` CNRs are terminal. Other phases, including `Healing`, are considered active.

## Configuration

The controller defaults are intentionally conservative:

- `--node-controller-reconcile-concurrency=1`
- `--node-controller-requeue-after=5m`

The controller is a safety net rather than a high-throughput reconciler, so one worker is normally enough. The requeue interval controls how often annotated nodes are rechecked while they are still covered by an active CNR.

## Observability

The node controller emits cleanup-specific metrics so we can tell when the backstop is doing work:

- `cyclops_node_cleanup_annotations_removed_total`
- `cyclops_node_cleanup_reconciles_total{result=...}`

A non-zero removal rate means stale annotations reached the node controller instead of being cleaned up by the normal CNR flow.
