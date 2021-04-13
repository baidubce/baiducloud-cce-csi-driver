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
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"

	cloudmock "github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud/mock"
	bosmock "github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/bos/mock"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
)

func TestNodeServer_NodePublishVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.NodeServer
	}

	normalReq := csi.NodePublishVolumeRequest{
		VolumeId:   "test-bucket/test/path",
		TargetPath: "/test/publish/target/path",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					MountFlags: []string{
						"-o meta_expires=0",
					},
				},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
			},
		},
		Readonly: false,
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
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
			name: "block volume is not supported",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := newNodeServer(nil, nil, nil)
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.NodePublishVolumeRequest{
				VolumeId:          "test-bucket/test/path",
				PublishContext:    nil,
				StagingTargetPath: "",
				TargetPath:        "/test/publish/target/path",
				VolumeCapability: &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Block{},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
				Readonly: false,
				Secrets: map[string]string{
					common.AccessKey: "test-ak",
					common.SecretKey: "test-sk",
				},
			},
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "failed to check bucket",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(false, fmt.Errorf("test"))
				server := newNodeServer(bosService, nil, nil)
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
			name: "bucket not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(false, nil)
				server := newNodeServer(bosService, nil, nil)
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
			name: "check target path whether exist failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(bosService, mounter, nil)
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
			name: "mkdir target path failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(bosService, mounter, nil)
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
			name: "mkdir credentials dir failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(fmt.Errorf("test"))
				server := newNodeServer(bosService, mounter, &common.DriverOptions{EndpointOptions: common.EndpointOptions{BOSEndpoint: "test.endpoint"}})
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
			name: "setup credentials file failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(nil)
				mounter.EXPECT().WriteFile(gomock.Any(), getCredentialsFilePath("/test/publish/target/path"), []byte(fmt.Sprintf(credentialsFileContentTmpl, "test-ak", "test-sk", ""))).Return(fmt.Errorf("test"))
				server := newNodeServer(bosService, mounter, &common.DriverOptions{EndpointOptions: common.EndpointOptions{BOSEndpoint: "test.endpoint"}})
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
			name: "mount by bosfs failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(nil)
				mounter.EXPECT().WriteFile(gomock.Any(), getCredentialsFilePath("/test/publish/target/path"), []byte(fmt.Sprintf(credentialsFileContentTmpl, "test-ak", "test-sk", ""))).Return(nil)
				mounter.EXPECT().MountByBOSFS(
					gomock.Any(),
					"test-bucket/test/path",
					"/test/publish/target/path",
					[]string{
						"-o tmpdir=/tmp",
						"-o credentials=" + getCredentialsFilePath("/test/publish/target/path"),
						"-o endpoint=test.endpoint",
						"-o meta_expires=0",
					},
					nil).Return(fmt.Errorf("test"))
				server := newNodeServer(bosService, mounter, &common.DriverOptions{EndpointOptions: common.EndpointOptions{BOSEndpoint: "test.endpoint"}})
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
			name: "mount succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				bosService := cloudmock.NewMockBOSService(ctrl)
				bosService.EXPECT().BucketExists(gomock.Any(), "test-bucket", gomock.Any()).Return(true, nil)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().MkdirAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(nil)
				mounter.EXPECT().WriteFile(gomock.Any(), getCredentialsFilePath("/test/publish/target/path"), []byte(fmt.Sprintf(credentialsFileContentTmpl, "test-ak", "test-sk", ""))).Return(nil)
				mounter.EXPECT().MountByBOSFS(
					gomock.Any(),
					"test-bucket/test/path",
					"/test/publish/target/path",
					[]string{
						"-o tmpdir=/tmp",
						"-o credentials=" + getCredentialsFilePath("/test/publish/target/path"),
						"-o endpoint=test.endpoint",
						"-o meta_expires=0",
					},
					nil).Return(nil)
				server := newNodeServer(bosService, mounter, &common.DriverOptions{EndpointOptions: common.EndpointOptions{BOSEndpoint: "test.endpoint"}})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
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
		VolumeId:   "test-bucket/test/path",
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
			name: "check target path failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("test"))
				server := newNodeServer(nil, mounter, nil)
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
			name: "target path transport endpoint is not connected and unmount failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(false, fmt.Errorf("transport endpoint is not connected"))
				mounter.EXPECT().UnmountFromBOSFS(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(nil, mounter, nil)
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
			name: "remove target path failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().UnmountFromBOSFS(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(fmt.Errorf("test"))
				server := newNodeServer(nil, mounter, nil)
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
			name: "check whether credentials dir exists failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().UnmountFromBOSFS(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().PathExists(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(false, fmt.Errorf("test"))
				server := newNodeServer(nil, mounter, nil)
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
			name: "credentials dir exists but remove failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().UnmountFromBOSFS(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().PathExists(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(true, nil)
				mounter.EXPECT().RemoveAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(fmt.Errorf("test"))
				server := newNodeServer(nil, mounter, nil)
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
			name: "credentials dir exists and remove succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				mounter := bosmock.NewMockMounter(ctrl)
				mounter.EXPECT().PathExists(gomock.Any(), "/test/publish/target/path").Return(true, nil)
				mounter.EXPECT().UnmountFromBOSFS(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().RemovePath(gomock.Any(), "/test/publish/target/path").Return(nil)
				mounter.EXPECT().PathExists(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(true, nil)
				mounter.EXPECT().RemoveAll(gomock.Any(), path.Dir(getCredentialsFilePath("/test/publish/target/path"))).Return(nil)
				server := newNodeServer(nil, mounter, nil)
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
