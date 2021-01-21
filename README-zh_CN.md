# 百度云 CSI 插件

[English](./README.md) | 简体中文

## 插件介绍

百度云 CSI 插件实现了在 Kubernetes 中对百度云云存储的生命周期管理。当前 CSI 实现基于 K8S 1.16 以上版本。

## 支持的特性

### CDS Driver

实现了下面的CSI gRPC接口：

* Controller Service: CreateVolume, DeleteVolume, ControllerPublishVolume, ControllerUnpublishVolume, ControllerGetCapabilities, ValidateVolumeCapabilities
* Node Service: NodeStageVolume, NodeUnstageVolume, NodePublishVolume, NodeUnpublishVolume, NodeGetCapabilities, NodeGetInfo
* Identity Service: GetPluginInfo, GetPluginCapabilities, Probe

实现的特性：

* 静态配置 - 支持使用已有的CDS盘进行PV的配置与创建。
* 动态配置 - 支持通过配置PVC来使得K8S动态根据PVC的配置来创建CDS盘，并且完成对应的PV的配置与创建。
* 挂载选项 - 支持在PV配置中设置 mount 命令的参数。
* 块设备 - 支持把PV作为原始块设备挂载进容器。

### BOS Driver

实现了下面的CSI gRPC接口：

* Node Service: NodePublishVolume, NodeUnpublishVolume, NodeGetCapabilities, NodeGetInfo
* Identity Service: GetPluginInfo, GetPluginCapabilities, Probe

实现的特性：

* 静态配置 - 支持使用已有的BOS Bucket进行PV的配置与创建。
* 挂载选项 - 支持在PV配置中设置 bosfs 的启动参数。

## 快速开始

本小节介绍如何在一个百度云 [CCE](https://cloud.baidu.com/product/cce.html) 集群中快速部署 CSI 插件。

### 前置条件

需要具备一个可用的百度云 CCE 集群。见[创建一个 CCE 集群](https://cloud.baidu.com/doc/CCE/s/zjxpoqohb).

### 部署

#### 部署 CDS Driver

```
kubectl apply -f ./deploy/kubernetes/cds/rbac.yaml
kubectl apply -f ./deploy/kubernetes/cds/controller.yaml
kubectl apply -f ./deploy/kubernetes/cds/node.yaml
```

#### 部署 BOS Driver

```
kubectl apply -f ./deploy/kubernetes/bos/rbac.yaml
kubectl apply -f ./deploy/kubernetes/bos/node.yaml
kubectl apply -f ./deploy/kubernetes/bos/csidriver.yaml
```

## 测试

### 单元测试

```
make test
```

## 如何贡献

请先阅读[CSI Spec](https://github.com/container-storage-interface/spec/blob/master/spec.md) 和 [General CSI driver development guideline](https://kubernetes-csi.github.io/docs/developing.html)，了解 CSI 的基本原理和开发指导。 

### 环境

* Golang 1.13.+
* Docker 17.05+ 用于镜像发布

### 依赖管理

Go module

### 镜像构建

```
export GO111MODULE=on
make image-all
```

### Issues

接受的 Issues 包括：

* 需求与建议。
* Bug。

### 维护者

* 主要维护者：: pansiyuan02@baidu.com, yezichao@baidu.com

### 讨论

* Issue 列表
* 如流群：1586317