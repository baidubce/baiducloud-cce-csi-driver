# Raw Block Volume

This example shows how to consume a dynamically-provisioned CDS volume as a raw block device.

## Prerequisites

* Kubernetes 1.16 +
* CSI CDS plugin install with `--auth-mode=key` and `--driver-type=cds`

## Usage

* Create AccessKey secret
```
kubectl create secret generic cds-access-key \
--from-literal=ak=<Your AK> \
--from-literal=sk=<Your SK>
```

* Create a sample App

```
kubectl apple -f specs/
```

* Clean resources

```
kubectl delete -f specs/
kubectl delete secret cds-access-key
```