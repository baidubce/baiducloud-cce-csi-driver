# Dynamic provisioning CDS volume with CCE Gateway

This example shows how to create a CDS volume and consume it from container dynamically with CCE Gateway.

## Prerequisites

* CCE cluster: Kubernetes 1.16+
* CSI CDS plugin install with `--auth-mode=gateway`, `--driver-type=cds` and `--cluster-id=<Your CCE cluster ID>`

## Usage

* Create a sample App

```
kubectl apple -f specs/
```

* Clean resources

```
kubectl delete -f specs/
```