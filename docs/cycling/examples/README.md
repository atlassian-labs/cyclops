# Cycling 

- [Cycling](#cycling)
  - [Example 1 - Basic Cycling](#example-1---basic-cycling)
    - [Metadata](#metadata)
    - [NodeGroupName](#nodegroupname)
    - [Node Selector](#node-selector)
    - [Cycle Settings](#cycle-settings)
    - [What happens](#what-happens)
  - [Example 2 - Concurrency and Multiple Selectors](#example-2---concurrency-and-multiple-selectors)
    - [What happens](#what-happens-1)
  - [Example 3 - Specific Nodes](#example-3---specific-nodes)
  - [Example 4 - Wait method](#example-4---wait-method)
  - [Example 5 - Concurrency within multiple cloud provider node groups](#example-5---concurrency-within-multiple-cloud-provider-node-groups)

See directory [examples](./examples) for all example CRDs

## Example 1 - Basic Cycling

```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "nodes.my-nodegroup.my-site"
  selector:
    matchLabels:
      role: node
  cycleSettings:
    method: Drain
```

This is the most basic of cycle node request.  It has 3 core components.

### Metadata

Create a unique name for your cycle request, as we want to scale on a fresh state.
The namespace should get set to the same namespace in which the Cyclops operator deployment is configured to watch.

```yaml
  metadata:
    name: "example"
    namespace: "kube-system"
```

### NodeGroupName

```yaml
    nodeGroupName: "nodes.my-nodegroup.my-site"
```
Selects the Cloud Provider auto scaling group to cycle nodes from. This is full name of the node group which is used to detach and terminate instances from.

### Node Selector

```yaml
  selector:
    matchLabels:
      role: node
```

### Cycle Settings

```yaml
  cycleSettings:
    method: Drain
```

This selector matches to a Kubernetes node selector. It is the method cyclops uses to match Kubernetes node group to cloud provider node group. The number of nodes in each must be the same, and Cyclops will wait a short period for this to become true, or fail.

In this example, our nodes have a selector `role`. This `role` has value `node`, which identifies this node as one that should be rotated, opposed to something like an ingress or system node.

### What happens

Cyclops will select all nodes that match the node selector `role=node` and with a one at a time, will increase the size of the autoscaling group on `nodes.my-nodegroup.my-site` to create a new node. Once the new node is in the `Ready` state, cyclops will cordon, detach, and begin draining (as it is the default termination method) a node. Once the node is drained, it will be removed from Kubernetes and terminated in the cloud provider. This repeats one by one for every node in the cluster.

## Example 2 - Concurrency and Multiple Selectors

```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "nodes.my-nodegroup.my-site"
  selector:
    matchLabels:
      role: node
      customer: shared
  cycleSettings:
    concurrency: 5
    method: Drain
```

This examples adds 2 components to the previous example. First, a concurrency value of 5, which tells Cyclops to cycle this node group 5 nodes at a time instead of 1. 

```yaml
  concurrency: 5
```
Is how this concurrency is specified.

Additionally in the example, an extra node selector is added to show that it's possible to further isolate which Kubernetes nodes belong in which cloud provider node group. This may be the case for example, running multi-tenanted customer work loads.

```yaml
  matchLabels:
      role: node
      customer: shared
```
Matches against the hypothetical `role` **and** `customer` node selectors.

### What happens

Cyclops will follow the same process as in Example 1, but 5 nodes at a time until all nodes have been cycled.

## Example 3 - Specific Nodes

```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "nodes.my-nodegroup.my-site"
  selector:
    matchLabels:
      role: node
      customer: shared
  nodeNames:
  - "my-node-1"
  - "my-node-2"
  cycleSettings:
    concurrency: 5
    method: Drain
```

This example will perform the same as **Example 2**, except it will limit the scope of the nodes to cycle to the node names in the `nodeNames:` list

```yaml
  nodeNames:
  - "my-node-1"
  - "my-node-2"
```

## Example 4 - Wait method


```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "nodes.my-nodegroup.my-site"
  selector:
    matchLabels:
      role: node
      customer: shared
  cycleSettings:
    method: "Wait"
    ignorePodsLabels:
      name:
        - stickypod  
```

This example shows the usage of the `Wait` method which opposed to `Drain` which attempts to remove pods from the node before terminating, will wait for all pods to leave the node naturally by themselves. This is useful for situations where you cannot forcefully remove pods, such as high churn jobs which need to be run to completion.

```yaml
  cycleSettings:
    method: "Wait"
    ignorePodsLabels:
      name:
        - stickypod  
```

Cyclops provides an option to ignore pods with specific labels in order to support nodes that may run pods that will never exit themselves. In this example, the pod with label `name=stickypod` would be ignore when waiting for all other pods to terminate. The node will be terminated while `name=stickypod` is running, and all others have finished.


## Example 5 - Concurrency within multiple cloud provider node groups


```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "az1-nodes.my-nodegroup.my-site"
  nodeGroupsList:
    ["az2-nodes.my-nodegroup.my-site", "az3-nodes.my-nodegroup.my-site"]
  selector:
    matchLabels:
      role: node
      customer: shared
  cycleSettings:
    concurrency: 1
    method: Drain 
```

This example shows the usage of the `nodeGroupsList` optional field with concurrency. Cyclops maintains same concurrency for all the nodes inside those `nodeGroupName` and `nodeGroupsList` cloud provider node groups. This is useful for situations where you need to split a group of node into different cloud provider node groups, e.g. split by availability zones due to autoscaling issue, and also want to maintain same concurrency for nodes in those groups especially a lower concurrency which cannot be achieved by creating multiple `NodeGroup` objects.

## Example 6 - Cycling with health checks enabled

```yaml
apiVersion: "atlassian.com/v1"
kind: "CycleNodeRequest"
metadata:
  name: "example"
  namespace: "kube-system"
spec:
  nodeGroupName: "az1-nodes.my-nodegroup.my-site"
  selector:
    matchLabels:
      role: node
      customer: shared
  cycleSettings:
    concurrency: 1
    method: Drain
  healthChecks:
  - endpoint: http://{{ .NodeIP }}:8080/ready
    regexMatch: Ready
    validStatusCodes:
    - 200
    waitPeriod: 5m
  - endpoint: http://service-name.namespace.svc.cluster.local:9090/ready
    validStatusCodes:
    - 200
    waitPeriod: 5m
```

Cyclops can optionally perform a set of health checks before each node selected in terminated. This can be useful to perform deep health checks on system daemons or pods running on host network to ensure they are healthy before continuing with cycling. The set of health checks will be performed until each returns a healthy status once. `{{ .NodeIP }}` can be use to render the endpoint with the private ip of a new instance brought up during the cycling.
