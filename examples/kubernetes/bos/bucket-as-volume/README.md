# BOS bucket as volume

This example shows how to create a BOS volume and consume it from container with Baidu Cloud AccessKey.

## Prerequisites

* Kubernetes 1.16 +
* CSI BOS plugin install with `--auth-mode=key` and `--driver-type=bos`

## Usage

* Create AccessKey secret
```
kubectl create secret generic bos-access-key \
  --from-literal=ak=<Your AK> \
  --from-literal=sk=<Your SK>
```

* Edit [Example PV](./specs/pv.yaml) to set `volumeHandle` to your BOS bucket name
```
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-bos-bucket
  namespace: "default"
spec:
  accessModes:
    - ReadWriteOnce
    - ReadOnlyMany
  capacity:
    storage: 5Gi
  csi:
    driver: "bos.csi.baidubce.com"
    volumeHandle: <Your BOS bucket>
    nodePublishSecretRef:
      name: "bos-access-key"
      namespace: "default"
  mountOptions:
    - "-o meta_expires=0"
  persistentVolumeReclaimPolicy: Retain
```

* Create a sample App

```
kubectl apple -f specs/
```

* Clean resources

```
kubectl delete -f specs/
kubectl delete secret bos-access-key
```