# Documents and Design

- [Documents and Design](#documents-and-design)
  - [Topics](#topics)
    - [Deployment](#deployment)
    - [Cycling Process](#cycling-process)
    - [Everyday Usage](#everyday-usage)
    - [Examples](#examples)
  - [Development Information](#development-information)
    - [Command Line](#command-line)
    - [Package Layout and Usage](#package-layout-and-usage)
    - [Changing or updating CRDs](#changing-or-updating-crds)
    - [Testing](#testing)
    - [Test a specific package](#test-a-specific-package)

## Topics

### Deployment

[Deployment](./deployment/README.md)

### Cycling Process

[Cycling Process](./cycling/README.md#process)

### kubectl-cycle CLI

[Automation CLI Usage](./automation/README.md#cli)

### Automated Cycling with Observer

[Automation Observer Usage](./automation/README.md#observer)

### Everyday Usage

[Usage](./cycling/README.md#usage)

### Examples

[Examples](./cycling/examples/README.md)

## Development Information

See various information pertaining to development below.

### Command Line

```bash
usage: Cyclops [<flags>]

Kubernetes operator to rotate a group of nodes

Flags:
      --help                     Show context-sensitive help (also try --help-long and --help-man).
      --version                  Show application version.
  -d, --debug                    Run with debug logging
      --cloud-provider="aws"     Which cloud provider to use, options: [aws]
      --address=":8080"          Address to listen on for /metrics
      --namespace="kube-system"  Namespace to watch for cycle request objects
```

### Package Layout and Usage
**TODO**: cyclops
- `cmd/manager`
    - contains command function, setup, and config loading
- `pkg/controller`
    - contains the core logic, state and transitions and reconciling CRDs
- `pkg/k8s`
    - provides application utils and help with interfacing with Kubernetes and client-go
- `pkg/cloudprovider`
    - provides everything related to cloud providers
    - `pkg/cloudprovider/aws`
      - provides the aws implementation of cloudprovider
- `pkg/metrics`
    - provides a place for all metric setup to live
- `pkg/apis`
    - schemes and code for generating Kubernetes CRD code

### Changing or updating CRDs

Whenever you update the CRD objects in the `pkg/apis/atlassian/v1` package you will likely need to generate the OpenAPI and Kubernetes deepcopy code again.

Run `make install-operator-sdk` if you haven't installed the `operator-sdk` tool yet.

Run `make generate-crds` to generate all the deepcopy and openapi code for the CRDs.  

### Testing
```bash
make test
```

### Test a specific package
For example, to test the controller package:

```bash
go test ./pkg/controller
```
