/*
 * Copyright (c) 2020 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package cds

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/volume"
	"k8s.io/kubernetes/pkg/volume/util/hostutil"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

const (
	defaultFSTypeEXT4 = "ext4"
)

var (
	// nodeCaps represents the capability of node service.
	nodeCaps = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	}
)

type nodeServer struct {
	csi.UnimplementedNodeServer

	options *common.NodeOptions

	mounter           Mounter
	inProcessRequests *util.KeyMutex
}

func newNodeServer(mounter Mounter, options *common.DriverOptions) csi.NodeServer {
	return &nodeServer{
		UnimplementedNodeServer: csi.UnimplementedNodeServer{},
		options:                 &options.NodeOptions,
		mounter:                 mounter,
		inProcessRequests:       util.NewKeyMutex(),
	}
}

func (server *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	var caps []*csi.NodeServiceCapability
	for _, cap := range nodeCaps {
		c := &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (server *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	resp := &csi.NodeGetInfoResponse{
		NodeId: server.options.NodeID,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				corev1.LabelFailureDomainBetaZone: server.options.Zone,
			},
		},
	}

	if server.options.MaxVolumesPerNode > 0 {
		resp.MaxVolumesPerNode = int64(server.options.MaxVolumesPerNode)
	}

	return resp, nil
}

func (server *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	// 1. Parse and check request arguments.
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetStagingTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty staging target path", ctx.Value(util.TraceIDKey))
	}

	volumeCap := req.GetVolumeCapability()
	if err := checkVolumeCapabilities([]*csi.VolumeCapability{volumeCap}); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	// We do not need to do anything if access type is block.
	if block := volumeCap.GetBlock(); block != nil {
		glog.V(4).Infof("[%s] Volume: %s access type is block, skip staging", ctx.Value(util.TraceIDKey), volumeID)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	mount := volumeCap.GetMount()
	if mount == nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] mount is nil in volume capability when staging", ctx.Value(util.TraceIDKey))
	}

	fsType := mount.GetFsType()
	if fsType == "" {
		glog.V(4).Infof("[%s] Use default fsType: %s in staging volume: %s", ctx.Value(util.TraceIDKey), defaultFSTypeEXT4, volumeID)
		fsType = defaultFSTypeEXT4
	}

	mountFlags := mount.GetMountFlags()

	publicContext := req.GetPublishContext()
	if publicContext == nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] nil volume publish context", ctx.Value(util.TraceIDKey))
	}

	serial := publicContext[SerialKey]
	if serial == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty serial in volume publish context", ctx.Value(util.TraceIDKey))
	}

	// 2. Check target path status and ensure target path exists.
	if locked := server.inProcessRequests.TryLock(req); !locked {
		glog.V(4).Infof("[%s] Request of NodeStageVolume: %s is in process, abort", ctx.Value(util.TraceIDKey), volumeID)
		return nil, status.Errorf(codes.Aborted, "[%s] request of NodeStageVolume: %s is in process, abort", ctx.Value(util.TraceIDKey), volumeID)
	}
	defer func() {
		server.inProcessRequests.Unlock(req)
		glog.V(4).Infof("[%s] Processing request of NodeStageVolume: %s is done", ctx.Value(util.TraceIDKey), volumeID)
	}()

	source, err := server.mounter.GetDevPath(ctx, serial)
	if err != nil {
		glog.Errorf("[%s] Failed to get dev path by serial, id: %s, serial: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, serial, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get dev path by serial, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether staging target path exists, id: %s, path: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether staging target path exists, path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if !exist {
		if err := server.mounter.MkdirAll(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
	}

	devPath, _, err := server.mounter.GetDeviceNameFromMount(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to get dev name from mount path: %s failed, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get dev name from mount path: %s failed, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if devPath == source {
		glog.V(4).Infof("[%s] Volume: %s is already mounted at staging target path: %s", ctx.Value(util.TraceIDKey), volumeID, targetPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	// 3. Format and mount.
	if err := server.mounter.FormatAndMount(ctx, source, targetPath, fsType, mountFlags); err != nil {
		glog.Errorf("[%s] Failed to format and mount volume: %s at staging target path: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to format and mount volume at staging target path, err: %v", ctx.Value(util.TraceIDKey), err)
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

func (server *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetStagingTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty staging target path", ctx.Value(util.TraceIDKey))
	}

	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether staging target path: %s exists, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether staging target path exists, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if !exist {
		glog.V(4).Infof("[%s] Staging target path is exists, skip unstaging", ctx.Value(util.TraceIDKey))
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	dev, refCount, err := server.mounter.GetDeviceNameFromMount(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check staging target path: %s mount status, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check staging target path mount status, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if refCount > 0 {
		if refCount > 1 {
			glog.Warningf("[%s] Mount reference count of dev: %s is %d, staging target path: %s", ctx.Value(util.TraceIDKey), dev, refCount, targetPath)
		}
		if err := server.mounter.Unmount(targetPath); err != nil {
			glog.Errorf("[%s] Failed to unmount staging target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to unmount staging target path, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}

	if err := server.mounter.RemovePath(ctx, targetPath); err != nil {
		glog.Errorf("[%s] Failed to remove staging target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to remove staging target path, err: %v", ctx.Value(util.TraceIDKey), err)
	}
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (server *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	volumeCap := req.GetVolumeCapability()
	if err := checkVolumeCapabilities([]*csi.VolumeCapability{volumeCap}); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	switch mode := volumeCap.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		return server.nodePublicBlockVolume(ctx, req, mode)
	case *csi.VolumeCapability_Mount:
		return server.nodePublishMountVolume(ctx, req, mode)
	default:
		return nil, status.Errorf(codes.InvalidArgument, "[%s] invalid volume access type", ctx.Value(util.TraceIDKey))
	}
}

func (server *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty unpublish target path", ctx.Value(util.TraceIDKey))
	}

	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether unpublish target path: %s exists, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether unpublish target path: %s exists, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}
	if !exist {
		glog.V(4).Infof("[%s] Unpublish target path: %s is exists, skip", ctx.Value(util.TraceIDKey), targetPath)
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	dev, _, err := server.mounter.GetDeviceNameFromMount(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if dev != "" {
		if err := server.mounter.Unmount(targetPath); err != nil {
			glog.Errorf("[%s] Failed to unmount unpublish target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to unmount target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
	}
	glog.V(4).Infof("[%s] Unpublish target path: %s is already unmounted", ctx.Value(util.TraceIDKey), targetPath)

	if err := server.mounter.RemovePath(ctx, targetPath); err != nil {
		glog.Errorf("[%s] Failed to remove unpublish target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to remove unpublish target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (server *nodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] volumeID is empty", ctx.Value(util.TraceIDKey))
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] volumePath is empty", ctx.Value(util.TraceIDKey))
	}

	glog.Infof("[%s] Getting volume stats, volume id: %s, volume path: %s", ctx.Value(util.TraceIDKey), volumeID, volumePath)

	exists, err := server.mounter.PathExists(ctx, volumePath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether volume path exists, err: %v", ctx.Value(util.TraceIDKey), err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether volume path exists, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if !exists {
		glog.Errorf("[%s] Volume path %s not exists", ctx.Value(util.TraceIDKey), volumePath)
		return nil, status.Errorf(codes.NotFound, "[%s] volume path %s not exists", ctx.Value(util.TraceIDKey), volumePath)
	}

	isDevice, err := hostutil.NewHostUtil().PathIsDevice(volumePath)
	if err != nil {
		glog.Errorf("[%s] Failed to checkout whether volume is device, err: %v", ctx.Value(util.TraceIDKey), err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to checkout whether volume is device, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if isDevice {
		size, err := server.getBlockDeviceSize(ctx, volumePath)
		if err != nil {
			glog.Errorf("[%s] Failed to get volume size, err: %v", ctx.Value(util.TraceIDKey), err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to get volume size, err: %v", ctx.Value(util.TraceIDKey), err)
		}

		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Unit:  csi.VolumeUsage_BYTES,
					Total: size,
				},
			},
		}, nil
	}

	metrics, err := volume.NewMetricsStatFS(volumePath).GetMetrics()
	if err != nil {
		glog.Errorf("[%s] Failed to get volume metrics, err: %v", ctx.Value(util.TraceIDKey), err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get volume metrics, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Unit:      csi.VolumeUsage_BYTES,
				Available: metrics.Available.AsDec().UnscaledBig().Int64(),
				Total:     metrics.Capacity.AsDec().UnscaledBig().Int64(),
				Used:      metrics.Used.AsDec().UnscaledBig().Int64(),
			},
			{
				Unit:      csi.VolumeUsage_INODES,
				Available: metrics.InodesFree.AsDec().UnscaledBig().Int64(),
				Total:     metrics.Inodes.AsDec().UnscaledBig().Int64(),
				Used:      metrics.InodesUsed.AsDec().UnscaledBig().Int64(),
			},
		},
	}, nil
}

func (server *nodeServer) nodePublicBlockVolume(ctx context.Context, req *csi.NodePublishVolumeRequest, mode *csi.VolumeCapability_Block) (*csi.NodePublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty publish target path", ctx.Value(util.TraceIDKey))
	}

	publicContext := req.GetPublishContext()
	if publicContext == nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] nil volume publish context", ctx.Value(util.TraceIDKey))
	}

	serial := publicContext[SerialKey]
	if serial == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty serial in volume publish context", ctx.Value(util.TraceIDKey))
	}

	source, err := server.mounter.GetDevPath(ctx, serial)
	if err != nil {
		glog.Errorf("[%s] Failed to get dev path by serial, id: %s, serial: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, serial, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get dev path by serial, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether publish target path exists, id: %s, path: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether publish target path exists, path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if !exist {
		targetPathDir := filepath.Dir(targetPath)
		if err := server.mounter.MkdirAll(ctx, targetPathDir); err != nil {
			glog.Errorf("[%s] Failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPathDir, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPathDir, err)
		}
		if err := server.mounter.MakeFile(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to make file: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to make file: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
	}

	dev, _, err := server.mounter.GetDeviceNameFromMount(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}
	if dev != "" {
		glog.V(4).Infof("[%s] Publish target path: %s is already mounted", ctx.Value(util.TraceIDKey), targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	mountOptions := []string{
		"bind",
	}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	glog.V(4).Infof("[%s] Going to mount %s to %s, mount options: %+v", ctx.Value(util.TraceIDKey), source, targetPath, mountOptions)
	if err := server.mounter.Mount(source, targetPath, "", mountOptions); err != nil {
		glog.Errorf("[%s] Failed to mount %s to %s, err: %v", ctx.Value(util.TraceIDKey), source, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to mount %s to %s, err: %v", ctx.Value(util.TraceIDKey), source, targetPath, err)
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (server *nodeServer) nodePublishMountVolume(ctx context.Context, req *csi.NodePublishVolumeRequest, mode *csi.VolumeCapability_Mount) (*csi.NodePublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	source := req.GetStagingTargetPath()
	if source == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty staging target path", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty publish target path", ctx.Value(util.TraceIDKey))
	}

	fsType := mode.Mount.GetFsType()
	if fsType == "" {
		fsType = defaultFSTypeEXT4
	}

	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether publish target path exists, id: %s, path: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether publish target path exists, path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if !exist {
		if err := server.mounter.MkdirAll(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
	}

	dev, _, err := server.mounter.GetDeviceNameFromMount(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check mounted dev of target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}
	if dev != "" {
		glog.V(4).Infof("[%s] Publish target path: %s is already mounted", ctx.Value(util.TraceIDKey), targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}

	mountOptions := []string{
		"bind",
	}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}
	mountOptions = append(mountOptions, mode.Mount.GetMountFlags()...)

	glog.V(4).Infof("[%s] Going to mount %s to %s, mount options: %+v", ctx.Value(util.TraceIDKey), source, targetPath, mountOptions)
	if err := server.mounter.Mount(source, targetPath, "", mountOptions); err != nil {
		glog.Errorf("[%s] Failed to mount %s to %s, err: %v", ctx.Value(util.TraceIDKey), source, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to mount %s to %s, err: %v", ctx.Value(util.TraceIDKey), source, targetPath, err)
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (server *nodeServer) getBlockDeviceSize(ctx context.Context, path string) (int64, error) {
	cmd := server.mounter.Command("blockdev", "--getsize64", path)
	output, err := cmd.Output()
	if err != nil {
		glog.Errorf("[%s] Failed to get size of volume, path: %s, output: %s, err: %v", ctx.Value(util.TraceIDKey), path, output, err)
		return -1, err
	}

	trimSpaceOutput := strings.TrimSpace(string(output))
	size, err := strconv.ParseInt(trimSpaceOutput, 10, 64)
	if err != nil {
		glog.Errorf("[%s] Failed to parse size %s as int, err: %v", ctx.Value(util.TraceIDKey), trimSpaceOutput, err)
		return -1, err
	}
	return size, nil
}
