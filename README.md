# Baidu Cloud CSI Plugin

English | [简体中文](./README-zh_CN.md)

## Introduction

Baidu Cloud CSI plugins implement an interface between CSI enabled Container
Orchestrator and Baidu Cloud Storage.

## Features

### CDS Driver

The following CSI gRPC calls are implemented:

* Controller Service: CreateVolume, DeleteVolume, ControllerPublishVolume, ControllerUnpublishVolume, ControllerGetCapabilities, ValidateVolumeCapabilities
* Node Service: NodeStageVolume, NodeUnstageVolume, NodePublishVolume, NodeUnpublishVolume, NodeGetCapabilities, NodeGetInfo
* Identity Service: GetPluginInfo, GetPluginCapabilities, Probe

Features:

* Static Provisioning - create persistence volume (PV) from the existing CDS volume and consume the PV from container using persistence volume claim (PVC).
* Dynamic Provisioning - uses persistence volume claim (PVC) to request the Kuberenetes to create the CDS volume on behalf of user and consumes the volume from inside container.
* Mount Option - mount options could be specified in persistence volume (PV) to define how the volume should be mounted.
* Block Volume - consumes the CDS volume as a raw block device for latency sensitive application. The corresponding CSI feature (CSIBlockVolume) is GA since Kubernetes 1.18.

### BOS Driver

The following CSI gRPC calls are implemented:

* Node Service: NodePublishVolume, NodeUnpublishVolume, NodeGetCapabilities, NodeGetInfo
* Identity Service: GetPluginInfo, GetPluginCapabilities, Probe

Features:

* Static Provisioning - create persistence volume (PV) from the existing BOS bucket and consume the PV from container using persistence volume claim (PVC).
* Mount Option - mount options could be specified in persistence volume (PV) to define how the bosfs should be run.

## Getting Started

These instructions will get you a copy of the project up and running on your environment for development and testing purposes. See installing for notes on how to deploy the project on a [Baidu Cloud CCE](https://cloud.baidu.com/product/cce.html) cluster.

### Prerequisites

A health CCE kubernetes cluster. See [documents for creating a CCE cluster](https://cloud.baidu.com/doc/CCE/s/zjxpoqohb).

### Installing

#### Install CSI CDSPlugin
```
kubectl apply -f ./deploy/kubernetes/cds/rbac.yaml
kubectl apply -f ./deploy/kubernetes/cds/controller.yaml
kubectl apply -f ./deploy/kubernetes/cds/node.yaml
```

#### Install CSI BOSPlugin
```
kubectl apply -f ./deploy/kubernetes/bos/rbac.yaml
kubectl apply -f ./deploy/kubernetes/bos/node.yaml
kubectl apply -f ./deploy/kubernetes/bos/csidriver.yaml
```

## Running the tests

### Unit Test

```
make test
```

## Contributing

Please go through [CSI Spec](https://github.com/container-storage-interface/spec/blob/master/spec.md) and [General CSI driver development guideline](https://kubernetes-csi.github.io/docs/developing.html) to get some basic understanding of CSI driver before you start.

### Requirements
* Golang 1.13.+
* Docker 17.05+ for releasing

### Dependency
Dependencies are managed through go module. 

### Build
To build the project, first turn on go mod using `export GO111MODULE=on`, then build the docker image using: `make image-all`.

### Issues

* Please create an issue in issue list.
* Contact Committers/Owners for further discussion if needed.

## Authors

* Maintainers: pansiyuan02@baidu.com, yezichao@baidu.com

## Discussion

* Issue list.
* Ruliu Group ID: 1586317