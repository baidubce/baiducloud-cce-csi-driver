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
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/assert"

	cdsmock "github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/cds/mock"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

func TestNodeServer_NodeStageVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}

	normalReq := csi.NodeStageVolumeRequest{
		VolumeId: "v-xxxx",
		PublishContext: map[string]string{
			SerialKey: "v-xxxx",
		},
		StagingTargetPath: "/test/stage/target/path",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.NodeStageVolumeRequest
		expectedResp *csi.NodeStageVolumeResponse
		expectedErr  bool
	}{
		{
			name: "volume capability not matches",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := newNodeServer(nil, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.NodeStageVolumeRequest{
				VolumeId: "v-xxxx",
				PublishContext: map[string]string{
					SerialKey: "v-xxxx",
				},
				StagingTargetPath: "/test/stage/target/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "stage block volume",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := newNodeServer(nil, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.NodeStageVolumeRequest{
				VolumeId: "v-xxxx",
				PublishContext: map[string]string{
					SerialKey: "v-xxxx",
				},
				StagingTargetPath: "/test/stage/target/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			expectedResp: &csi.NodeStageVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "request is in process",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := &nodeServer{
					UnimplementedNodeServer: csi.UnimplementedNodeServer{},
					options:                 nil,
					mounter:                 nil,
					inProcessRequests:       util.NewKeyMutex(),
				}
				server.inProcessRequests.TryLock(&normalReq)
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to get src dev path by serial",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("", fmt.Errorf("serial not found"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check whether target path exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to create target path",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/stage/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check target path mount status",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/stage/target/path").Return(nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("", 0, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "dev already mounts to target path",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/stage/target/path").Return(nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("/dev/vdc", 1, nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeStageVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to format ot mount dev",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/stage/target/path").Return(nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("", 0, nil)
				mounter.EXPECT().FormatAndMount(gomock.Any(), "/dev/vdc", "/test/stage/target/path", "ext4", nil).Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "succeed to format and mount dev",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/stage/target/path").Return(nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("", 0, nil)
				mounter.EXPECT().FormatAndMount(gomock.Any(), "/dev/vdc", "/test/stage/target/path", "ext4", nil).Return(nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeStageVolumeResponse{},
			expectedErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.NodeStageVolume(context.Background(), tc.req)
			if (err != nil && !tc.expectedErr) || (err == nil && tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
				return
			}
			if !cmp.Equal(resp, tc.expectedResp) {
				t.Errorf("expected resp: %v, actual: %v, diff: %s", tc.expectedResp, resp, cmp.Diff(tc.expectedResp, resp))
			}
		})
	}
}

func TestNodeServer_NodeUnstageVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}

	normalReq := csi.NodeUnstageVolumeRequest{
		VolumeId:          "v-xxxx",
		StagingTargetPath: "/test/stage/target/path",
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.NodeUnstageVolumeRequest
		expectedResp *csi.NodeUnstageVolumeResponse
		expectedErr  bool
	}{
		{
			name: "failed to check whether target path exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "target path not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(false, nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeUnstageVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check target path mount status",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("", 0, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "target path is already unmounted, but remove target path failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("", 0, nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/stage/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "unmounts target path failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("/dev/vdc", 1, nil)
				mounter.EXPECT().Unmount("/test/stage/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "unmounts and removes target path succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/stage/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/stage/target/path").Return("/dev/vdc", 1, nil)
				mounter.EXPECT().Unmount("/test/stage/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/stage/target/path").Return(nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeUnstageVolumeResponse{},
			expectedErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.NodeUnstageVolume(context.Background(), tc.req)
			if (err != nil && !tc.expectedErr) || (err == nil && tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
				return
			}
			if !cmp.Equal(resp, tc.expectedResp) {
				t.Errorf("expected resp: %v, actual: %v, diff: %s", tc.expectedResp, resp, cmp.Diff(tc.expectedResp, resp))
			}
		})
	}
}

func TestNodeServer_NodePublishVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}

	normalMountReq := csi.NodePublishVolumeRequest{
		VolumeId: "v-xxxx",
		PublishContext: map[string]string{
			SerialKey: "v-xxxx",
		},
		StagingTargetPath: "/test/stage/target/path",
		TargetPath:        "/test/publish/target/path",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		Readonly:      false,
		Secrets:       nil,
		VolumeContext: nil,
	}

	normalBlockReq := csi.NodePublishVolumeRequest{
		VolumeId: "v-xxxx",
		PublishContext: map[string]string{
			SerialKey: "v-xxxx",
		},
		StagingTargetPath: "/test/stage/target/path",
		TargetPath:        "/test/publish/target/path",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		Readonly:      false,
		Secrets:       nil,
		VolumeContext: nil,
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.NodePublishVolumeRequest
		expectedResp *csi.NodePublishVolumeResponse
		expectedErr  bool
	}{
		{
			name: "volume capability not matches",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := newNodeServer(nil, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.NodePublishVolumeRequest{
				VolumeId: "v-xxxx",
				PublishContext: map[string]string{
					SerialKey: "v-xxxx",
				},
				StagingTargetPath: "/test/stage/target/path",
				TargetPath:        "/test/publish/target/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							FsType: "ext4",
						},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
				Readonly:      false,
				Secrets:       nil,
				VolumeContext: nil,
			},
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to get src dev path by serial. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("", fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check whether publish target path exists. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to create publish target path dir. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/publish/target").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to create publish target path. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/publish/target").Return(nil)
				mounter.EXPECT().MakeFile(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check target path mount status. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "publish target path is already mounted. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("tmpfs", 1, nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: &csi.NodePublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "bind mount failed. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, nil)
				mounter.EXPECT().Mount("/dev/vdc", "/test/publish/target/path", "", []string{"bind"}).Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "bind mount succeed. (block)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDevPath(gomock.Any(), "v-xxxx").Return("/dev/vdc", nil)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, nil)
				mounter.EXPECT().Mount("/dev/vdc", "/test/publish/target/path", "", []string{"bind"}).Return(nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalBlockReq,
			expectedResp: &csi.NodePublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check whether target path exists. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to create target path. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check target path mount status. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "publish target path is already mounted. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("tmpfs", 1, nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: &csi.NodePublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "bind mount failed. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, nil)
				mounter.EXPECT().Mount("/test/stage/target/path", "/test/publish/target/path", "", []string{"bind"}).Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "bind mount succeed. (mount)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, nil)
				mounter.EXPECT().Mount("/test/stage/target/path", "/test/publish/target/path", "", []string{"bind"}).Return(nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalMountReq,
			expectedResp: &csi.NodePublishVolumeResponse{},
			expectedErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.NodePublishVolume(context.Background(), tc.req)
			if (err != nil && !tc.expectedErr) || (err == nil && tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
				return
			}
			if !cmp.Equal(resp, tc.expectedResp) {
				t.Errorf("expected resp: %v, actual: %v, diff: %s", tc.expectedResp, resp, cmp.Diff(tc.expectedResp, resp))
			}
		})
	}
}

func TestNodeServer_NodeUnpublishVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}

	normalReq := csi.NodeUnpublishVolumeRequest{
		VolumeId:   "v-xxxx",
		TargetPath: "/test/publish/target/path",
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.NodeUnpublishVolumeRequest
		expectedResp *csi.NodeUnpublishVolumeResponse
		expectedErr  bool
	}{
		{
			name: "failed to check whether target path exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "target path not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeUnpublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check target path mount status",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("", 0, fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to unmount target path",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("tmpfs", 1, nil)
				mounter.EXPECT().Unmount("/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to remove target path",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("tmpfs", 1, nil)
				mounter.EXPECT().Unmount("/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "unmount and remove target path succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/publish/target/path").Return("tmpfs", 1, nil)
				mounter.EXPECT().Unmount("/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(nil)
				server := newNodeServer(mounter, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.NodeUnpublishVolumeResponse{},
			expectedErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.NodeUnpublishVolume(context.Background(), tc.req)
			if (err != nil && !tc.expectedErr) || (err == nil && tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
				return
			}
			if !cmp.Equal(resp, tc.expectedResp) {
				t.Errorf("expected resp: %v, actual: %v, diff: %s", tc.expectedResp, resp, cmp.Diff(tc.expectedResp, resp))
			}
		})
	}
}

func TestNodeServer_NodeExpandVolume(t *testing.T) {
	normalExpandRequest := &csi.NodeExpandVolumeRequest{
		VolumeId:   "v-xxxx",
		VolumePath: "/test/volume/path",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10 * util.GB,
			LimitBytes:    10 * util.GB,
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
		},
	}

	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}
	type args struct {
		ctx context.Context
		req *csi.NodeExpandVolumeRequest
	}
	tests := []struct {
		name    string
		mocks   mocks
		args    args
		want    *csi.NodeExpandVolumeResponse
		wantErr string
	}{
		// TODO: Add test cases.
		{
			name: "successfully expand volume case",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				gomock.InOrder(
					mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/volume/path").Return("/dev/vdb", 2, nil),
					mounter.EXPECT().GetDeviceSize(gomock.Any(), "/dev/vdb").Return(int64(10*util.GB), nil),
					mounter.EXPECT().ResizeFS(gomock.Any(), "/dev/vdb", "/test/volume/path"),
				)

				server := newNodeServer(mounter, &common.DriverOptions{})

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: normalExpandRequest,
			},
			want: &csi.NodeExpandVolumeResponse{
				CapacityBytes: util.GB * 10,
			},
		},
		{
			name: "resizefs failed case",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				gomock.InOrder(
					mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/volume/path").Return("/dev/vdb", 2, nil),
					mounter.EXPECT().GetDeviceSize(gomock.Any(), "/dev/vdb").Return(int64(10*util.GB), nil),
					mounter.EXPECT().ResizeFS(gomock.Any(), "/dev/vdb", "/test/volume/path").Return(fmt.Errorf("resizefs failed")),
				)

				server := newNodeServer(mounter, &common.DriverOptions{})

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: normalExpandRequest,
			},
			wantErr: "resizefs failed",
		},
		{
			name: "device size does not meet capacity range case",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				gomock.InOrder(
					mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/volume/path").Return("/dev/vdb", 2, nil),
					mounter.EXPECT().GetDeviceSize(gomock.Any(), "/dev/vdb").Return(int64(12*util.GB), nil),
				)

				server := newNodeServer(mounter, &common.DriverOptions{})

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: normalExpandRequest,
			},
			wantErr: "does not meet capacityRange",
		},
		{
			name: "get device size failed case",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				gomock.InOrder(
					mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/volume/path").Return("/dev/vdb", 2, nil),
					mounter.EXPECT().GetDeviceSize(gomock.Any(), "/dev/vdb").Return(int64(0), fmt.Errorf("get device size failed")),
				)

				server := newNodeServer(mounter, &common.DriverOptions{})

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: normalExpandRequest,
			},
			wantErr: "get device size failed",
		},
		{
			name: "get device path from volume path failed case",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := cdsmock.NewMockMounter(ctrl)
				mounter.EXPECT().GetDeviceNameFromMount(gomock.Any(), "/test/volume/path").Return("", 0, fmt.Errorf("some error"))

				server := newNodeServer(mounter, &common.DriverOptions{})

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: normalExpandRequest,
			},
			wantErr: "some error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mocks.ctrl != nil {
				defer tt.mocks.ctrl.Finish()
			}
			got, err := tt.mocks.server.NodeExpandVolume(tt.args.ctx, tt.args.req)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			}

			assert.DeepEqual(t, got, tt.want)
		})
	}
}
