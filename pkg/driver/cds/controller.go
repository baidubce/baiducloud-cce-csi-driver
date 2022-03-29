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
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

const (
	defaultVolumeSizeGB = 100
)

var (
	// volumeCaps represents how the volume could be accessed.
	// Baiducloud CDS can be attached to only one node.
	volumeCaps = []csi.VolumeCapability_AccessMode{
		{
			Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		},
	}

	// controllerCaps represents the capability of controller service
	controllerCaps = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	}

	// createVolumeGroup implements idempotency with volumeName.
	createVolumeGroup singleflight.Group
	// controllerPublishVolumeGroup implements idempotency with volumeID.
	controllerPublishVolumeGroup singleflight.Group
	// controllerUnpublishVolumeGroup implements idempotency with volumeID.
	controllerUnpublishVolumeGroup singleflight.Group
)

type controllerServer struct {
	csi.UnimplementedControllerServer

	options *common.ControllerOptions

	volumeService         cloud.CDSVolumeService
	nodeService           cloud.NodeService
	getAuth               func(map[string]string) (cloud.Auth, error)
	creatingVolumeCache   *sync.Map
	inProcessRequests     *util.KeyMutex
	enableOnlineExpansion bool
}

func newControllerServer(volumeService cloud.CDSVolumeService, nodeService cloud.NodeService, options *common.DriverOptions) csi.ControllerServer {
	var getAuth func(map[string]string) (cloud.Auth, error)
	switch options.AuthMode {
	case cloud.AuthModeAccessKey:
		getAuth = common.GetAuthModeAccessKey
	case cloud.AuthModeCCEGateway:
		getAuth = func(map[string]string) (cloud.Auth, error) {
			return cloud.NewCCEGatewayAuth(options.Region, options.ClusterID)
		}
	default:
		getAuth = common.GetAuthModeAccessKey
	}

	return &controllerServer{
		UnimplementedControllerServer: csi.UnimplementedControllerServer{},
		options:                       &options.ControllerOptions,
		volumeService:                 volumeService,
		nodeService:                   nodeService,
		getAuth:                       getAuth,
		creatingVolumeCache:           &sync.Map{},
		inProcessRequests:             util.NewKeyMutex(),
		enableOnlineExpansion:         options.EnableOnlineExpansion,
	}
}

func (server *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	var caps []*csi.ControllerServiceCapability
	for _, cap := range controllerCaps {
		caps = append(caps, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: caps,
	}, nil
}

// retryOnAborted triggers retry on status Aborted with context awared.
func retryOnAborted[Req any, Resp any](ctx context.Context, name string, req Req, group *singleflight.Group, key string, f func(context.Context, Req) (Resp, error)) (interface{}, error, bool) {
	return group.Do(key, func() (resp interface{}, err error) {
		var cancel context.CancelFunc
		deadline, ok := ctx.Deadline()
		if !ok {
			ctx, cancel = context.WithTimeout(ctx, time.Minute)
			defer cancel()
			deadline, _ = ctx.Deadline()
		}

		// Retry should be interrupted a little earlier before the overall context deadline,
		// or the http request in flight may be canceled, which would mess up the real error.
		waitDeadline := deadline.Add(-3 * time.Second)
		glog.V(4).Infof("[%s] %s will wait until %s", ctx.Value(util.TraceIDKey), name, waitDeadline.Format(time.RFC3339Nano))
		waitCtx, waitCancel := context.WithDeadline(context.TODO(), waitDeadline)
		defer waitCancel()

		// Retry loop with deadline.
	RetryLoop:
		for {
			resp, err = f(ctx, req)
			if status.Code(err) != codes.Aborted {
				// Do not retry on codes other than aborted.
				break RetryLoop
			}
			glog.Warningf("[%s] Request of %s %s is aborted, may retry in 2s: %v",
				ctx.Value(util.TraceIDKey), name, key, err)
			select {
			case <-waitCtx.Done():
				glog.Warningf("retry context has exceeded: %v", waitCtx.Err())
				break RetryLoop
			case <-time.After(2 * time.Second):
			}
		}
		return resp, err
	})
}

func (server *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	volumeName := req.GetName()
	if volumeName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume name", ctx.Value(util.TraceIDKey))
	}
	// createVolumeGroup implements idempotency with volumeName.
	result, err, shared := retryOnAborted(ctx, "CreateVolume", req, &createVolumeGroup, volumeName, server.createVolume)
	glog.V(4).Infof("[%s] Request of CreateVolume %s returns with shared=%v err=%v",
		ctx.Value(util.TraceIDKey), volumeName, shared, err)
	resp := result.(*csi.CreateVolumeResponse)
	return resp, err
}

func (server *controllerServer) createVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	// 1. Parse and check request arguments.
	volumeName := req.GetName()
	if volumeName == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume name", ctx.Value(util.TraceIDKey))
	}

	// In case of reentrant and result in multi volume created.
	if succeed := server.inProcessRequests.TryLock(req); !succeed {
		glog.V(4).Infof("[%s] Request of CreateVolume: %s is in process, abort", ctx.Value(util.TraceIDKey), volumeName)
		return nil, status.Errorf(codes.Aborted, "[%s] request of CreateVolume: %s is in process, abort", ctx.Value(util.TraceIDKey), volumeName)
	}
	defer func() {
		server.inProcessRequests.Unlock(req)
		glog.V(4).Infof("[%s] Processing request of CreateVolume: %s is done", ctx.Value(util.TraceIDKey), volumeName)
	}()

	if err := checkVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] check volume capabilities failed, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	volumeSizeGB, err := getVolumeSizeGB(req.GetCapacityRange())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] invalid volume size, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	zone := getVolumeZone(req.GetAccessibilityRequirements())

	var (
		storageType         string
		paymentTiming       string
		reservationLength   int
		reservationTimeUnit string
	)
	for key, value := range req.GetParameters() {
		switch key {
		case StorageTypeKey:
			storageType = value
			if value == "ssd" {
				// compatibility storage type: ssd => cloud_hp1
				storageType = "cloud_hp1"
			}
		case PaymentTimingKey:
			paymentTiming = value
		case ReservationLengthKey:
			if value != "" {
				reservationLength, err = strconv.Atoi(value)
				if err != nil {
					return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("[%s] invalid reservationLength, %v", ctx.Value(util.TraceIDKey), err))
				}
			}
		case ReservationTimeUnitKey:
			reservationTimeUnit = value
		default:
			glog.Warningf("[%s] Unsupported volume param found: %s: %s", ctx.Value(util.TraceIDKey), key, value)
		}
	}

	if storageType == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty storage type", ctx.Value(util.TraceIDKey))
	}
	if paymentTiming == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty payment timing", ctx.Value(util.TraceIDKey))
	}

	glog.V(4).Infof("[%s] Creating volume, volume name: %s, volumeSizeGB: %d, zone: %s, storageType: %s,"+
		" paymentTiming: %s, reservationLength: %d", ctx.Value(util.TraceIDKey), volumeName, volumeSizeGB, zone,
		storageType, paymentTiming, reservationLength)

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	// 2. Check whether volume is exist by getting volume by name.
	volume, err := server.volumeService.GetVolumeByName(ctx, volumeName, auth)
	if err != nil {
		switch {
		case cloud.ErrIsNotFound(err):
			if _, found := server.creatingVolumeCache.Load(volumeName); found {
				// Volume is creating, but failed to fetch because of DB synchronization. Just return error and CO will retry.
				glog.Errorf("[%s] Volume name is in creating volume cache, but is not found from cloud, name: %s",
					ctx.Value(util.TraceIDKey), volumeName)
				return nil, status.Errorf(codes.Aborted, "[%s] volume is creating, retry later", ctx.Value(util.TraceIDKey))
			}
		default:
			glog.Errorf("[%s] Failed to get cds volume by name, name: %s, err: %v", ctx.Value(util.TraceIDKey), volumeName, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to check volume by name, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}

	if volume != nil {
		glog.V(4).Infof("[%s] Volume exists, name: %s, volume detail: %s", ctx.Value(util.TraceIDKey),
			volumeName, util.ToJSONOrPanic(volume.Detail()))
		defer server.creatingVolumeCache.Delete(volumeName)
		// Volume exists
		sizeGB := volume.SizeGB()
		if sizeGB != volumeSizeGB {
			glog.Errorf("[%s] Volume %s already exists, but has a different size, volume size: %d GB", ctx.Value(util.TraceIDKey), volumeName, volume.SizeGB())
			return nil, status.Errorf(codes.AlreadyExists, "[%s] volume is already exist, but has a different size", ctx.Value(util.TraceIDKey))
		}

		if volume.IsCreating() {
			glog.Errorf("[%s] Volume %s is creating, retry later", ctx.Value(util.TraceIDKey), volumeName)
			return nil, status.Errorf(codes.Aborted, "[%s] volume is creating, retry later", ctx.Value(util.TraceIDKey))
		}

		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				CapacityBytes: int64(sizeGB) * util.GB,
				VolumeId:      volume.ID(),
				AccessibleTopology: []*csi.Topology{
					{
						Segments: map[string]string{
							corev1.LabelFailureDomainBetaZone: volume.Zone(),
						},
					},
				},
			},
		}, nil
	}

	// 3. Create new volume
	args := &cloud.CreateCDSVolumeArgs{
		Name:                volumeName,
		ZoneName:            zone,
		CdsSizeInGB:         volumeSizeGB,
		StorageType:         storageType,
		ClientToken:         volumeName,
		PaymentTiming:       paymentTiming,
		ReservationLength:   reservationLength,
		ReservationTimeUnit: reservationTimeUnit,
		Tags:                map[string]string{},
	}

	if server.options.ClusterID != "" {
		args.ClientToken = server.options.ClusterID + "-" + volumeName
		args.Tags[ClusterIDTagKey] = server.options.ClusterID
	}
	args.Tags[VolumeNameTagKey] = volumeName

	glog.V(4).Infof("[%s] Going to create cds volume with args: %s", ctx.Value(util.TraceIDKey), util.ToJSONOrPanic(args))
	id, err := server.volumeService.CreateVolume(ctx, args, auth)
	if err != nil {
		glog.Errorf("[%s] Failed to create cds volume, name: %s, err: %v", ctx.Value(util.TraceIDKey), volumeName, err)
		switch {
		case cloud.ErrIsInvalidArgument(err):
			return nil, status.Errorf(codes.InvalidArgument, "[%s] failed to create volume on cloud, err: %v", ctx.Value(util.TraceIDKey), err)
		default:
			return nil, status.Errorf(codes.Internal, "[%s] failed to create volume on cloud, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}
	server.creatingVolumeCache.Store(volumeName, struct{}{})

	// 4. Fetch volume info
	volume, err = server.volumeService.GetVolumeByID(ctx, id, auth)
	if err != nil {
		switch {
		case cloud.ErrIsNotFound(err):
			// We found that it will failed to fetch volume info by id after volume creating,
			// because the time cost of DB synchronization.
			glog.Errorf("[%s] Volume is not found from cloud after creating, name: %s, id: %s",
				ctx.Value(util.TraceIDKey), volumeName, id)
			return nil, status.Errorf(codes.Aborted, "[%s] volume is creating, retry later", ctx.Value(util.TraceIDKey))
		default:
			glog.Errorf("[%s] Failed to fetch volume after creating, name: %s, id: %s, err: %v",
				ctx.Value(util.TraceIDKey), volumeName, id, err)
			return nil, status.Errorf(codes.Internal, "[%s] failed to check volume by id, err: %v", ctx.Value(util.TraceIDKey), err)
		}
	}
	// Delete volume name from cache after we can fetch volume from cloud.
	defer server.creatingVolumeCache.Delete(volumeName)
	glog.V(4).Infof("[%s] Fetch volume from cloud after creating, name: %s, id: %s, volume detail: %s",
		ctx.Value(util.TraceIDKey), volumeName, id, util.ToJSONOrPanic(volume.Detail()))

	if volume.IsCreating() {
		glog.Errorf("[%s] Volume %s is creating, retry later", ctx.Value(util.TraceIDKey), volumeName)
		return nil, status.Errorf(codes.Aborted, "[%s] volume is creating, retry later", ctx.Value(util.TraceIDKey))
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: int64(volume.SizeGB()) * util.GB,
			VolumeId:      volume.ID(),
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						corev1.LabelFailureDomainBetaZone: volume.Zone(),
					},
				},
			},
		},
	}, nil
}

func (server *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	volume, err := server.volumeService.GetVolumeByID(ctx, volumeID, auth)
	if err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.V(4).Infof("[%s] Volume had been deleted, id: %s", ctx.Value(util.TraceIDKey), volumeID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		glog.Errorf("[%s] Failed to describe volume info from cloud before deleting it, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to describe volume info from cloud, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	glog.V(4).Infof("[%s] Volume exists, start to delete it, id: %s, detail: %s", ctx.Value(util.TraceIDKey), volumeID, util.ToJSONOrPanic(volume.Detail()))

	if err := volume.Delete(ctx); err != nil {
		glog.Errorf("[%s] Failed to delete volume, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to delete volume, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (server *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}
	// controllerPublishVolumeGroup implements idempotency with volumeID.
	result, err, shared := retryOnAborted(ctx, "ControllerPublishVolume", req, &controllerPublishVolumeGroup, volumeID, server.controllerPublishVolume)
	glog.V(4).Infof("[%s] Request of ControllerPublishVolume %s returns with shared=%v err=%v",
		ctx.Value(util.TraceIDKey), volumeID, shared, err)
	resp := result.(*csi.ControllerPublishVolumeResponse)
	return resp, err
}

func (server *controllerServer) controllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// 1. Parse request args.
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}
	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty node id", ctx.Value(util.TraceIDKey))
	}

	if err := checkVolumeCapabilities([]*csi.VolumeCapability{req.GetVolumeCapability()}); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] check volume capabilities failed, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	glog.V(4).Infof("[%s] Attaching volume: %s to node: %s", ctx.Value(util.TraceIDKey), volumeID, nodeID)

	// 2. Check status of volume and node.
	if _, err := server.nodeService.GetNodeByID(ctx, nodeID, auth); err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.Errorf("[%s] Node not exists, id: %s", ctx.Value(util.TraceIDKey), nodeID)
			return nil, status.Errorf(codes.NotFound, "[%s] node: %s not exists", ctx.Value(util.TraceIDKey), nodeID)
		}
		glog.Errorf("[%s] Failed to get node from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), nodeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get node from cloud, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	volume, err := server.volumeService.GetVolumeByID(ctx, volumeID, auth)
	if err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.Errorf("[%s] Volume not exists, id: %s", ctx.Value(util.TraceIDKey), volumeID)
			return nil, status.Errorf(codes.NotFound, "[%s] volume: %s not exists", ctx.Value(util.TraceIDKey), volumeID)
		}
		glog.Errorf("[%s] Failed to get volume from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get volume from cloud, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if volume.IsAttached() {
		isAttachedToNode, devName, serial := isAttachedToNode(volume, nodeID)
		if isAttachedToNode {
			glog.V(4).Infof("[%s] Volume had been attached to node", ctx.Value(util.TraceIDKey))
			return &csi.ControllerPublishVolumeResponse{
				PublishContext: map[string]string{
					DevNameKey: devName,
					SerialKey:  serial,
				},
			}, nil
		}
		glog.Errorf("[%s] Volume had been attached to another node, volume id: %s, attachments: %s",
			ctx.Value(util.TraceIDKey), volumeID, util.ToJSONOrPanic(volume.Detail().Attachments))
		return nil, status.Errorf(codes.FailedPrecondition,
			"[%s] volume had been attached to another node, attachments: %s", ctx.Value(util.TraceIDKey), util.ToJSONOrPanic(volume.Detail().Attachments))
	}

	if volume.IsAttaching() {
		glog.V(4).Infof("[%s] Volume is attaching, id: %s", ctx.Value(util.TraceIDKey), volumeID)
		return nil, status.Errorf(codes.Aborted, "[%s] volume is attaching", ctx.Value(util.TraceIDKey))
	}

	if !volume.IsAvailable() {
		glog.Errorf("[%s] Volume is not available for attaching, id: %s, status: %s", ctx.Value(util.TraceIDKey), volumeID, volume.Detail().Status)
		return nil, status.Errorf(codes.Unavailable, "[%s] volume is not available for attaching, status: %s", ctx.Value(util.TraceIDKey), volume.Detail().Status)
	}

	// 3. Attach volume to node.
	if _, err := volume.Attach(ctx, nodeID); err != nil {
		glog.Errorf("[%s] Failed to attach volume: %s to node: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, nodeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to attach volume to node, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	// Attaching is asynchronous. Let CO to retry before volume has been attached.
	return nil, status.Errorf(codes.Aborted, "[%s] volume is attaching", ctx.Value(util.TraceIDKey))
}

func (server *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}
	// controllerUnpublishVolumeGroup implements idempotency with volumeID.
	result, err, shared := retryOnAborted(ctx, "ControllerUnpublishVolume", req, &controllerUnpublishVolumeGroup, volumeID, server.controllerUnpublishVolume)
	glog.V(4).Infof("[%s] Request of ControllerUnpublishVolume %s returns with shared=%v err=%v",
		ctx.Value(util.TraceIDKey), volumeID, shared, err)
	resp := result.(*csi.ControllerUnpublishVolumeResponse)
	return resp, err
}

func (server *controllerServer) controllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	// 1. Parse request args.
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty volume id", ctx.Value(util.TraceIDKey))
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] empty node id", ctx.Value(util.TraceIDKey))
	}

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] %v", ctx.Value(util.TraceIDKey), err)
	}

	glog.V(4).Infof("[%s] Going to detach volume: %s from node: %s", ctx.Value(util.TraceIDKey), volumeID, nodeID)

	// 2. Check volume and node status.
	volume, err := server.volumeService.GetVolumeByID(ctx, volumeID, auth)
	if err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.V(4).Infof("[%s] Volume not exists, which assumes that the detachment is succeed, id: %s", ctx.Value(util.TraceIDKey), volumeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		glog.Errorf("[%s] Failed to get volume from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get volume from cloud, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if _, err := server.nodeService.GetNodeByID(ctx, nodeID, auth); err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.V(4).Infof("[%s] Node not exists, which assumes that the detachment is succeed, id: %s", ctx.Value(util.TraceIDKey), nodeID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		glog.Errorf("[%s] Failed to get node from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), nodeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to get node from cloud, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	if isAttachedToNode, _, _ := isAttachedToNode(volume, nodeID); !isAttachedToNode {
		glog.V(4).Infof("[%s] Volume: %s had been detached from node: %s", ctx.Value(util.TraceIDKey), volumeID, nodeID)
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	if volume.IsDetaching() {
		glog.V(4).Infof("[%s] Volume is detaching, id: %s", ctx.Value(util.TraceIDKey), volumeID)
		return nil, status.Errorf(codes.Aborted, "[%s] volume is detaching", ctx.Value(util.TraceIDKey))
	}

	// 3. Detach volume from node
	if err := volume.Detach(ctx, nodeID); err != nil {
		glog.Errorf("[%s] Failed to detach volume: %s from node: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, nodeID, err)
		return nil, status.Errorf(codes.Internal, "[%s] failed to detach volume from node, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	// Detaching is asynchronous. Let CO to retry before volume has been detached.
	return nil, status.Errorf(codes.Aborted, "[%s] volume is detaching", ctx.Value(util.TraceIDKey))
}

func (server *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume id")
	}

	// volumeCap is optional for ControllerExpandVolumeRequest so it is not enforced
	var isBlock bool
	if volumeCap := req.GetVolumeCapability(); volumeCap != nil {
		if block := volumeCap.GetBlock(); block != nil {
			isBlock = true
		}
	}

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// Check whether volume exists.
	volume, err := server.volumeService.GetVolumeByID(ctx, volumeID, auth)
	if err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.Errorf("[%s] Volume: %s not exists", ctx.Value(util.TraceIDKey), volumeID)
			return nil, status.Error(codes.NotFound, "volume not exists")
		}
		glog.Errorf("[%s] Failed to get volume from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "failed to get volume from cloud, err: %v", err)
	}

	// Check if volume is scaling.
	if volume.IsScaling() {
		return nil, status.Errorf(codes.Aborted, "[%s] volume is scaling", ctx.Value(util.TraceIDKey))
	}

	capRange := req.GetCapacityRange()
	if capRange == nil {
		glog.Error("CapacityRange cannot be nil")
		return nil, status.Error(codes.InvalidArgument, "CapacityRange cannot be nil")
	}
	oldSizeInGB := volume.SizeGB()
	if sizeInGBMeetCapRange(int64(oldSizeInGB), capRange) {
		// volume size has met CapacityRange, no-op
		glog.V(4).Infof("[%s] disk size=%dGB has met capacityRange, skip resizing",
			ctx.Value(util.TraceIDKey), oldSizeInGB)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         util.SizeGBToBytes(int64(oldSizeInGB)),
			NodeExpansionRequired: !isBlock,
		}, nil
	}
	newSizeInGB, err := getVolumeSizeGB(capRange)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] Invalid volume size, err: %v", ctx.Value(util.TraceIDKey), err)
	}
	if newSizeInGB < oldSizeInGB {
		return nil, status.Errorf(codes.InvalidArgument, "[%s] Invalid want size %dGB: must be no less than current size=%dGB", ctx.Value(util.TraceIDKey), newSizeInGB, oldSizeInGB)
	}

	// Check if volume status is ready for expansion.
	canResize := (volume.IsAvailable() || (server.enableOnlineExpansion && volume.IsInUse()))
	if !canResize {
		volumeStatus := volume.Detail().Status
		glog.Errorf("[%s] Abort expansion for volume %s due to its status is %s", ctx.Value(util.TraceIDKey), volumeID, volumeStatus)
		return nil, status.Errorf(codes.FailedPrecondition, "[%s] Abort expansion for volume %s due to its status is %s", ctx.Value(util.TraceIDKey), volumeID, volumeStatus)
	}

	// Do cds resize.
	glog.V(4).Infof("[%s] Going to resize cds volume %s to %d GB", ctx.Value(util.TraceIDKey), volumeID, newSizeInGB)
	args := &cloud.ResizeCSDVolumeArgs{
		NewVolumeType:  volume.Detail().StorageType, // retain storageType
		NewCdsSizeInGB: newSizeInGB,
	}
	if err := volume.Resize(ctx, args); err != nil {
		glog.Errorf("[%s] Failed to resize volume=%s to %+v, err: %v", ctx.Value(util.TraceIDKey), volumeID, args, err)
		// TODO: check out of range error
		return nil, status.Errorf(codes.Internal, "[%s] Failed to resize volume, err: %v", ctx.Value(util.TraceIDKey), err)
	}

	// Resize is asynchronous. Let CO to retry before volume has been resized.
	return nil, status.Errorf(codes.Aborted, "[%s] volume is scaling", ctx.Value(util.TraceIDKey))
}

func (server *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty volume id")
	}

	// Check volume capabilities.
	if err := checkVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	auth, err := server.getAuth(req.GetSecrets())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, err.Error())
	}

	// Check whether volume exists.
	if _, err := server.volumeService.GetVolumeByID(ctx, volumeID, auth); err != nil {
		if cloud.ErrIsNotFound(err) {
			glog.Errorf("[%s] Volume: %s not exists", ctx.Value(util.TraceIDKey), volumeID)
			return nil, status.Error(codes.NotFound, "volume not exists")
		}
		glog.Errorf("[%s] Failed to get volume from cloud, id: %s, err: %v", ctx.Value(util.TraceIDKey), volumeID, err)
		return nil, status.Errorf(codes.Internal, "failed to get volume from cloud, err: %v", err)
	}

	// TODO: Check volume spec.

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
	}, nil
}

func getVolumeSizeGB(capRange *csi.CapacityRange) (int, error) {
	if capRange == nil {
		return defaultVolumeSizeGB, nil
	}

	sizeRequiredBytes := capRange.GetRequiredBytes()
	sizeLimitBytes := capRange.GetLimitBytes()

	if sizeLimitBytes > 0 && sizeLimitBytes < sizeRequiredBytes {
		return 0, fmt.Errorf("volume sizeLimitBytes < sizeRequiredBytes")
	}

	if sizeRequiredBytes == 0 {
		if sizeLimitBytes > 0 && util.SizeGBToBytes(defaultVolumeSizeGB) > sizeLimitBytes {
			return 0, fmt.Errorf("required volume size is not provided and default value is over capacity limit")
		}
		return defaultVolumeSizeGB, nil
	}

	sizeGB := util.RoundUpGB(sizeRequiredBytes)
	if sizeLimitBytes > 0 && util.SizeGBToBytes(sizeGB) > sizeLimitBytes {
		return 0, fmt.Errorf("volume size after rounding up to GB is over capacity limit")
	}
	return int(util.RoundUpGB(sizeRequiredBytes)), nil
}

func sizeInGBMeetCapRange(sizeInGB int64, capRange *csi.CapacityRange) bool {
	if capRange == nil {
		return false
	}

	sizeInBytes := util.SizeGBToBytes(sizeInGB)
	sizeRequiredBytes := capRange.GetRequiredBytes()
	sizeLimitBytes := capRange.GetLimitBytes()
	if sizeLimitBytes == 0 {
		sizeLimitBytes = sizeInBytes
	}
	return sizeInBytes >= sizeRequiredBytes && sizeInBytes <= sizeLimitBytes
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
			return fmt.Errorf("unsupported volume access mode, only ReadWriteOnce is supported")
		}
	}
	return nil
}

func getVolumeZone(topoRequirement *csi.TopologyRequirement) string {
	if topoRequirement == nil {
		return ""
	}

	for _, topo := range topoRequirement.GetPreferred() {
		if zone, exist := topo.Segments[corev1.LabelZoneFailureDomain]; exist {
			return zone
		}
	}
	for _, topo := range topoRequirement.GetRequisite() {
		if zone, exist := topo.Segments[corev1.LabelZoneFailureDomain]; exist {
			return zone
		}
	}
	return ""
}

func isAttachedToNode(volume cloud.CDSVolume, nodeID string) (bool, string, string) {
	for _, attachment := range volume.Detail().Attachments {
		if attachment.InstanceId == nodeID {
			return true, attachment.Device, attachment.Serial
		}
	}
	return false, "", ""
}
