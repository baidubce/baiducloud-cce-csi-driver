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

package bos

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

var (
	// volumeCaps represents how the volume could be accessed.
	volumeCaps = []csi.VolumeCapability_AccessMode{
		{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
		{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		},
		{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		},
		{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
		},
		{
			Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
		},
	}

	// nodeCaps represents the capability of node service.
	nodeCaps = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_UNKNOWN,
	}
)

type nodeServer struct {
	csi.UnimplementedNodeServer

	options *common.DriverOptions

	bosService cloud.BOSService
	getAuth    func(map[string]string) (cloud.Auth, error)
	mounter    Mounter
}

func newNodeServer(bosService cloud.BOSService, mounter Mounter, options *common.DriverOptions) csi.NodeServer {
	return &nodeServer{
		UnimplementedNodeServer: csi.UnimplementedNodeServer{},
		options:                 options,
		bosService:              bosService,
		getAuth:                 common.GetAuthModeAccessKey,
		mounter:                 mounter,
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
	}

	if server.options.MaxVolumesPerNode > 0 {
		resp.MaxVolumesPerNode = int64(server.options.MaxVolumesPerNode)
	}

	return resp, nil
}

func (server *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	// 1. Parse and check request arguments.

	// Source to be published. Can be BOS {bucket name} or {bucket name}/{object prefix}.
	source := req.GetVolumeId()
	if source == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty bos bucket source", ctx.Value(util.TraceIDKey))
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty publish target path", ctx.Value(util.TraceIDKey))
	}

	volumeCap := req.GetVolumeCapability()
	if err := checkVolumeCapabilities([]*csi.VolumeCapability{volumeCap}); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "[%s] check volume capabilities failed, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if block := volumeCap.GetBlock(); block != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] publishing as block volume is not supported", ctx.Value(util.TraceIDKey))
	}

	mount := volumeCap.GetMount()
	if mount == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "[%s] nil mount volume capability", ctx.Value(util.TraceIDKey))
	}
	mountFlags := mount.GetMountFlags()

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	bucket := bucketName(source)
	if bucket == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] invalid bucket source: %s", ctx.Value(util.TraceIDKey), source)
	}

	glog.V(4).Infof("[%s] Check whether bucket: %s exists", ctx.Value(util.TraceIDKey), bucket)
	exists, err := server.bosService.BucketExists(ctx, bucket, auth)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether bucket exists, bucket: %s, err: %v", ctx.Value(util.TraceIDKey), bucket, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether bucket: %s exists, err: %v", ctx.Value(util.TraceIDKey), bucket, err)
	}

	if !exists {
		glog.Errorf("[%s] Bucket: %s not exists", ctx.Value(util.TraceIDKey), bucket)
		return nil, status.Errorf(codes.NotFound, "[%s] bucket: %s not exists", ctx.Value(util.TraceIDKey), bucket)
	}

	// 2. Check and setup mount point.
	glog.V(4).Infof("[%s] Ensure target path: %s exists", ctx.Value(util.TraceIDKey), targetPath)
	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether publish target path exists, bucket source: %s, path: %s, err: %v", ctx.Value(util.TraceIDKey), source, targetPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether publish target path exists, path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
	}

	if !exist {
		if err := server.mounter.MkdirAll(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to make dir: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
	}

	// 3. Mount target path.
	options := []string{
		"-o tmpdir=" + bosfsTmpDir,
	}
	var sensitiveOptions []string

	// Set access key and secret key if provided.
	credentialsFilePath := getCredentialsFilePath(targetPath)
	if err := server.mounter.MkdirAll(ctx, path.Dir(credentialsFilePath)); err != nil {
		glog.Errorf("[%s] Failed to mkdir %s for bosfs credentials, err: %v", ctx.Value(util.TraceIDKey), path.Dir(credentialsFilePath), err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to mkdir for bosfs credentials, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if cred := auth.GetCredentials(ctx); cred != nil {
		// Setup bosfs credentials file.
		options = append(options, "-o credentials="+credentialsFilePath)
		if err := server.mounter.WriteFile(ctx, credentialsFilePath, []byte(fmt.Sprintf(credentialsFileContentTmpl, cred.AccessKeyId, cred.SecretAccessKey, cred.SessionToken))); err != nil {
			glog.Errorf("[%s] Failed to setup credentials file for bosfs, file path: %s, err: %v", ctx.Value(util.TraceIDKey), credentialsFilePath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to setup credentials file for bosfs, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}

	if req.GetReadonly() {
		options = append(options, "-o ro")
	}

	endpointSetByMountFlag := false
	for _, option := range mountFlags {
		if strings.HasPrefix(option, "-o endpoint=") {
			endpointSetByMountFlag = true
			break
		}
	}
	if !endpointSetByMountFlag {
		options = append(options, "-o endpoint="+server.options.BOSEndpoint)
	}
	options = append(options, mountFlags...)

	glog.V(4).Infof("[%s] Bosfs mount options: %v", ctx.Value(util.TraceIDKey), options)
	if err := server.mounter.MountByBOSFS(ctx, source, targetPath, options, sensitiveOptions); err != nil {
		glog.Errorf("[%s] Failed to mount target path, err: %v", ctx.Value(util.TraceIDKey), err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to mount target path, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	glog.V(4).Infof("[%s] Bucket source: %s mounts to %s successfully", ctx.Value(util.TraceIDKey), source, targetPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (server *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty unpublish target path", ctx.Value(util.TraceIDKey))
	}

	credentialsFileDirPath := path.Dir(getCredentialsFilePath(targetPath))
	exist, err := server.mounter.PathExists(ctx, targetPath)
	if err != nil {
		if !isErrTransportEndpointIsNotConnected(err) {
			glog.Errorf("[%s] Failed to check whether unpublish target path: %s exists, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to check whether unpublish target path: %s exists, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
		}
		// 'Transport endpoint is not connected' means path exists and should be unmounted.
		glog.V(4).Infof("[%s] Target path: %s transport endpoint is not connected", ctx.Value(util.TraceIDKey), targetPath)
		exist = true
	}

	// Target path exists, unmount and remove it.
	if exist {
		glog.V(4).Infof("[%s] Unmount target path: %s", ctx.Value(util.TraceIDKey), targetPath)
		if err := server.mounter.UnmountFromBOSFS(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to unmount from bosfs, target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to unmount target path, err: %v", ctx.Value(util.TraceIDKey), err)
		}

		glog.V(4).Infof("[%s] Remove target path: %s", ctx.Value(util.TraceIDKey), targetPath)
		if err := server.mounter.RemovePath(ctx, targetPath); err != nil {
			glog.Errorf("[%s] Failed to remove target path: %s, err: %v", ctx.Value(util.TraceIDKey), targetPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to remove target path, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}

	// Clean paths used by bosfs.
	exist, err = server.mounter.PathExists(ctx, credentialsFileDirPath)
	if err != nil {
		glog.Errorf("[%s] Failed to check whether bosfs credentials file dir: %s exits, err: %v", ctx.Value(util.TraceIDKey), credentialsFileDirPath, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to check whether bosfs credentials file dir exists, err: %v", ctx.Value(util.TraceIDKey), err)
	}
	if exist {
		glog.V(4).Infof("[%s] Remove bosfs credentials file dir: %s", ctx.Value(util.TraceIDKey), credentialsFileDirPath)
		if err := server.mounter.RemoveAll(ctx, credentialsFileDirPath); err != nil {
			glog.Errorf("[%s] Failed to remove bosfs credentials file dir: %s, err: %v", ctx.Value(util.TraceIDKey), credentialsFileDirPath, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to remove bosfs credentials file dir, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func checkVolumeCapabilities(requiredCaps []*csi.VolumeCapability) error {
	if len(requiredCaps) == 0 {
		return fmt.Errorf("empty volume capabilities")
	}

	for _, requiredCap := range requiredCaps {
		if supported := func(requiredCap *csi.VolumeCapability) bool {
			for _, cap := range volumeCaps {
				if cap.GetMode() == requiredCap.GetAccessMode().GetMode() {
					return true
				}
			}
			return false
		}(requiredCap); !supported {
			return fmt.Errorf("unsupported volume access mode")
		}
	}
	return nil
}

func bucketName(source string) string {
	parts := strings.Split(source, "/")
	return parts[0]
}
