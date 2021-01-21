# Raw Block Volume

This example shows how to consume a dynamically-provisioned CDS volume as a raw block device.

## Prerequisites

* Kubernetes 1.16 +
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