# Deployment

- [Deployment](#deployment)
    - [Deployment in Cluster](#deployment-in-cluster)
  - [Cloud Providers<a name="cloud-provider"></a>](#cloud-providersa-name%22cloud-provider%22a)
  - [Setup](#setup)
    - [Kubernetes API Config](#kubernetes-api-config)
    - [Create the Customer Resource Definitions](#create-the-customer-resource-definitions)
    - [RBAC<a name="rbac"></a>](#rbaca-name%22rbac%22a)
    - [Create the operator deployment](#create-the-operator-deployment)

### Deployment in Cluster

## Cloud Providers<a name="cloud-provider"></a>

Cyclops provides integration for the following cloud providers:

 - **AWS** - [see documentation](./aws/README.md)
   - Permissions
   - AWS Credentials
   - Node Group Configuration
   - Common issues, caveats and gotchas

## Setup

Cyclops runs as an operator inside the cluster, which watches Custom Resource Definitions. It needs the following resources to be applied in the cluster.

### Kubernetes API Config

When running inside the cluster, Cyclops will use the following for accessing the Kubernetes API:

```go
config, err := rest.InClusterConfig()
``` 

`rest.InClusterConfig()` uses the service account token inside the pod at 
`/var/run/secrets/kubernetes.io/serviceaccount` to gain access to the Kubernetes API. See 
[Authenticating inside the cluster](https://github.com/kubernetes/client-go/tree/master/examples/in-cluster-client-configuration).

Cyclops will need certain permissions to list/patch/get/watch/update/delete pods and nodes. See the section below on 
[RBAC](#rbac) to set up the service account, cluster role and cluster role binding.

### Create the Customer Resource Definitions 

In order for Kubernetes to recognise the resources Cyclops uses to handle requests and maintain state in the cluster over reschedules, we need to tell Kubernetes about our CRD.

To create the Custom Resource Definitions, run the following:

```bash
kubectl create -f deploy/crds/
```

### RBAC<a name="rbac"></a>

To be able to function correctly, Cyclops needs a service account with the following permissions:

- **pods**:
  - watch
  - list
  - get
  - update
  - delete
  - patch
- **nodes**: 
  - update
  - patch
  - watch
  - list
  - get
  - delete
- **pods/eviction**
  - create
- **events**
  - create
  - patch
- **atlassian.com/***
  - All permissions - "*"
    
To create the service account, cluster role and cluster role binding, run the following:


```bash
kubectl create -f docs/deployment/cyclops-rbac.yaml
```

### Create the operator deployment

This deployment makes use of the RBAC service account

To create the deployment, run the following:
```bash
kubectl create -f docs/deployment/cyclops-operator.yaml
```

**See [Cloud Provider documentation](#cloud-provider) for deployments specific to a cloud provider.**
