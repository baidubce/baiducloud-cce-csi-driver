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
	"net/http"
	"sync"
	"testing"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	cloudmock "github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud/mock"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
	"github.com/baidubce/bce-sdk-go/bce"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestControllerServer_CreateVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.ControllerServer
	}

	normalReq := csi.CreateVolumeRequest{
		Name: "pv-xxxx",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: util.GB,
			LimitBytes:    util.GB,
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		Parameters: map[string]string{
			StorageTypeKey:   "hp1",
			PaymentTimingKey: "Postpaid",
		},
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
		VolumeContentSource: nil,
		AccessibilityRequirements: &csi.TopologyRequirement{
			Requisite: []*csi.Topology{
				{
					Segments: map[string]string{
						corev1.LabelZoneFailureDomain: "zoneA",
					},
				},
			},
			Preferred: nil,
		},
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.CreateVolumeRequest
		expectedResp *csi.CreateVolumeResponse
		expectedErr  bool
	}{
		{
			name: "request is in process",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := &controllerServer{
					inProcessRequests: util.NewKeyMutex(),
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
			name: "volume capability not matches",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := &controllerServer{
					inProcessRequests: util.NewKeyMutex(),
				}
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.CreateVolumeRequest{
				Name: "pv-xxxx",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: util.GB,
					LimitBytes:    util.GB,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
						},
					},
				},
				Parameters: map[string]string{
					StorageTypeKey:   "hp1",
					PaymentTimingKey: "Postpaid",
				},
				Secrets: map[string]string{
					common.AccessKey: "test-ak",
					common.SecretKey: "test-sk",
				},
				VolumeContentSource: nil,
				AccessibilityRequirements: &csi.TopologyRequirement{
					Requisite: []*csi.Topology{
						{
							Segments: map[string]string{
								corev1.LabelZoneFailureDomain: "zoneA",
							},
						},
					},
					Preferred: nil,
				},
			},
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "volume is not found in cloud, but creating cache is set. (DB synchronization)",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					volumeService:                 volumeService,
					getAuth:                       common.GetAuthModeAccessKey,
					creatingVolumeCache:           &sync.Map{},
					inProcessRequests:             util.NewKeyMutex(),
				}
				server.creatingVolumeCache.Store("pv-xxxx", struct{}{})
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
			name: "failed to check volume whether exist in cloud",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					volumeService:                 volumeService,
					getAuth:                       common.GetAuthModeAccessKey,
					creatingVolumeCache:           &sync.Map{},
					inProcessRequests:             util.NewKeyMutex(),
				}

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
			name: "volume is already created",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(nil)
				cdsVolume.EXPECT().SizeGB().Return(1)
				cdsVolume.EXPECT().ID().Return("v-xxxx")
				cdsVolume.EXPECT().Zone().Return("zoneA")
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(cdsVolume, nil)
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					volumeService:                 volumeService,
					getAuth:                       common.GetAuthModeAccessKey,
					creatingVolumeCache:           &sync.Map{},
					inProcessRequests:             util.NewKeyMutex(),
				}

				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &normalReq,
			expectedResp: &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					CapacityBytes: 1 * util.GB,
					VolumeId:      "v-xxxx",
					AccessibleTopology: []*csi.Topology{
						{
							Segments: map[string]string{
								corev1.LabelFailureDomainBetaZone: "zoneA",
							},
						},
					},
				},
			},
			expectedErr: false,
		},
		{
			name: "create volume failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				volumeService.EXPECT().CreateVolume(gomock.Any(), gomock.Eq(&cloud.CreateCDSVolumeArgs{
					Name:                "pv-xxxx",
					Description:         "",
					SnapshotID:          "",
					ZoneName:            "zoneA",
					CdsSizeInGB:         1,
					StorageType:         "hp1",
					EncryptKey:          "",
					ClientToken:         "cce-xxxx-pv-xxxx",
					PaymentTiming:       "Postpaid",
					ReservationLength:   0,
					ReservationTimeUnit: "",
					Tags: map[string]string{
						ClusterIDTagKey:  "cce-xxxx",
						VolumeNameTagKey: "pv-xxxx",
					},
				}), gomock.Any()).Return("", &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}

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
			name: "volume is not found after creating",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				volumeService.EXPECT().CreateVolume(gomock.Any(), gomock.Eq(&cloud.CreateCDSVolumeArgs{
					Name:                "pv-xxxx",
					Description:         "",
					SnapshotID:          "",
					ZoneName:            "zoneA",
					CdsSizeInGB:         1,
					StorageType:         "hp1",
					EncryptKey:          "",
					ClientToken:         "cce-xxxx-pv-xxxx",
					PaymentTiming:       "Postpaid",
					ReservationLength:   0,
					ReservationTimeUnit: "",
					Tags: map[string]string{
						ClusterIDTagKey:  "cce-xxxx",
						VolumeNameTagKey: "pv-xxxx",
					},
				}), gomock.Any()).Return("v-xxxx", nil)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
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
			name: "fetch volume info from cloud failed after creating",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				volumeService.EXPECT().CreateVolume(gomock.Any(), gomock.Eq(&cloud.CreateCDSVolumeArgs{
					Name:                "pv-xxxx",
					Description:         "",
					SnapshotID:          "",
					ZoneName:            "zoneA",
					CdsSizeInGB:         1,
					StorageType:         "hp1",
					EncryptKey:          "",
					ClientToken:         "cce-xxxx-pv-xxxx",
					PaymentTiming:       "Postpaid",
					ReservationLength:   0,
					ReservationTimeUnit: "",
					Tags: map[string]string{
						ClusterIDTagKey:  "cce-xxxx",
						VolumeNameTagKey: "pv-xxxx",
					},
				}), gomock.Any()).Return("v-xxxx", nil)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
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
			name: "create volume succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByName(gomock.Any(), gomock.Eq("pv-xxxx"), gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				volumeService.EXPECT().CreateVolume(gomock.Any(), gomock.Eq(&cloud.CreateCDSVolumeArgs{
					Name:                "pv-xxxx",
					Description:         "",
					SnapshotID:          "",
					ZoneName:            "zoneA",
					CdsSizeInGB:         1,
					StorageType:         "hp1",
					EncryptKey:          "",
					ClientToken:         "cce-xxxx-pv-xxxx",
					PaymentTiming:       "Postpaid",
					ReservationLength:   0,
					ReservationTimeUnit: "",
					Tags: map[string]string{
						ClusterIDTagKey:  "cce-xxxx",
						VolumeNameTagKey: "pv-xxxx",
					},
				}), gomock.Any()).Return("v-xxxx", nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(nil)
				cdsVolume.EXPECT().SizeGB().Return(1)
				cdsVolume.EXPECT().ID().Return("v-xxxx")
				cdsVolume.EXPECT().Zone().Return("zoneA")
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &normalReq,
			expectedResp: &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					CapacityBytes: 1 * util.GB,
					VolumeId:      "v-xxxx",
					AccessibleTopology: []*csi.Topology{
						{
							Segments: map[string]string{
								corev1.LabelFailureDomainBetaZone: "zoneA",
							},
						},
					},
				},
			},
			expectedErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.CreateVolume(context.Background(), tc.req)
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

func TestControllerServer_DeleteVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.ControllerServer
	}

	normalReq := csi.DeleteVolumeRequest{
		VolumeId: "v-xxxx",
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.DeleteVolumeRequest
		expectedResp *csi.DeleteVolumeResponse
		expectedErr  bool
	}{
		{
			name: "volume is already deleted",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.DeleteVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check whether volume exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
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
			name: "failed to delete volume",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(nil)
				cdsVolume.EXPECT().Delete(gomock.Any()).Return(fmt.Errorf("test"))
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
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
			name: "delete volume succeed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(nil)
				cdsVolume.EXPECT().Delete(gomock.Any()).Return(nil)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := &controllerServer{
					UnimplementedControllerServer: csi.UnimplementedControllerServer{},
					options: &common.ControllerOptions{
						ClusterID: "cce-xxxx",
					},
					volumeService:       volumeService,
					getAuth:             common.GetAuthModeAccessKey,
					creatingVolumeCache: &sync.Map{},
					inProcessRequests:   util.NewKeyMutex(),
				}
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.DeleteVolumeResponse{},
			expectedErr:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.DeleteVolume(context.Background(), tc.req)
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

func TestControllerServer_ControllerPublishVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.ControllerServer
	}

	normalReq := csi.ControllerPublishVolumeRequest{
		VolumeId: "v-xxxx",
		NodeId:   "i-xxxx",
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.ControllerPublishVolumeRequest
		expectedResp *csi.ControllerPublishVolumeResponse
		expectedErr  bool
	}{
		{
			name: "volume capability not matches",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				server := newControllerServer(nil, nil, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &csi.ControllerPublishVolumeRequest{
				VolumeId: "v-xxxx",
				NodeId:   "i-xxxx",
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
				Secrets: map[string]string{
					common.AccessKey: "test-ak",
					common.SecretKey: "test-sk",
				},
			},
			expectedResp: nil,
			expectedErr:  true,
		},
		{
			name: "node not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := newControllerServer(nil, nodeService, &common.DriverOptions{})
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
			name: "failed to check whether node exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := newControllerServer(nil, nodeService, &common.DriverOptions{})
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
			name: "volume not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "failed to check whether volume exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "volume is already attached to the node",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsAttached().Return(true)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:     "v-xxxx",
					Status: bccapi.VolumeStatusINUSE,
					Attachments: []bccapi.VolumeAttachmentModel{
						{
							VolumeId:   "v-xxxx",
							InstanceId: "i-xxxx",
							Device:     "/dev/vdc",
							Serial:     "v-xxxx",
						},
					},
				}).AnyTimes()
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req: &normalReq,
			expectedResp: &csi.ControllerPublishVolumeResponse{
				PublishContext: map[string]string{
					DevNameKey: "/dev/vdc",
					SerialKey:  "v-xxxx",
				},
			},
			expectedErr: false,
		},
		{
			name: "volume is already attached to other node",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsAttached().Return(true)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:     "v-xxxx",
					Status: bccapi.VolumeStatusINUSE,
					Attachments: []bccapi.VolumeAttachmentModel{
						{
							VolumeId:   "v-xxxx",
							InstanceId: "i-xxxx-2",
							Device:     "/dev/vdc",
							Serial:     "v-xxxx",
						},
					},
				}).AnyTimes()
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "volume is attaching",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsAttached().Return(false)
				cdsVolume.EXPECT().IsAttaching().Return(true)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "failed to attach volume",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsAttached().Return(false)
				cdsVolume.EXPECT().IsAttaching().Return(false)
				cdsVolume.EXPECT().IsAvailable().Return(true)
				cdsVolume.EXPECT().Attach(gomock.Any(), "i-xxxx").Return(nil, fmt.Errorf("test"))
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "succeed to attach volume, but not finish",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsAttached().Return(false)
				cdsVolume.EXPECT().IsAttaching().Return(false)
				cdsVolume.EXPECT().IsAvailable().Return(true)
				cdsVolume.EXPECT().Attach(gomock.Any(), "i-xxxx").Return(nil, nil)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.ControllerPublishVolume(context.Background(), tc.req)
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

func TestControllerServer_ControllerUnpublishVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.ControllerServer
	}

	normalReq := csi.ControllerUnpublishVolumeRequest{
		VolumeId: "v-xxxx",
		NodeId:   "i-xxxx",
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
	}

	testCases := []struct {
		name         string
		mocks        mocks
		req          *csi.ControllerUnpublishVolumeRequest
		expectedResp *csi.ControllerUnpublishVolumeResponse
		expectedErr  bool
	}{
		{
			name: "volume not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})

				server := newControllerServer(volumeService, nil, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.ControllerUnpublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check whether volume exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})

				server := newControllerServer(volumeService, nil, &common.DriverOptions{})
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
			name: "node not exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusNotFound})
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.ControllerUnpublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "failed to check whether node exists",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, &bce.BceServiceError{StatusCode: http.StatusBadRequest})
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "volume is not attached to the node",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:          "v-xxxx",
					Status:      bccapi.VolumeStatusAVAILABLE,
					Attachments: []bccapi.VolumeAttachmentModel{},
				}).AnyTimes()
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: &csi.ControllerUnpublishVolumeResponse{},
			expectedErr:  false,
		},
		{
			name: "volume is detaching",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:     "v-xxxx",
					Status: bccapi.VolumeStatusDETACHING,
					Attachments: []bccapi.VolumeAttachmentModel{
						{
							VolumeId:   "v-xxxx",
							InstanceId: "i-xxxx",
							Serial:     "v-xxxx",
							Device:     "/dev/vdc",
						},
					},
				}).AnyTimes()
				cdsVolume.EXPECT().IsDetaching().Return(true)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "failed to detach volume",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:     "v-xxxx",
					Status: bccapi.VolumeStatusINUSE,
					Attachments: []bccapi.VolumeAttachmentModel{
						{
							VolumeId:   "v-xxxx",
							InstanceId: "i-xxxx",
							Serial:     "v-xxxx",
							Device:     "/dev/vdc",
						},
					},
				}).AnyTimes()
				cdsVolume.EXPECT().IsDetaching().Return(false)
				cdsVolume.EXPECT().Detach(gomock.Any(), "i-xxxx").Return(fmt.Errorf("test"))
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
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
			name: "succeed to detach, but not finish",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
					Id:     "v-xxxx",
					Status: bccapi.VolumeStatusINUSE,
					Attachments: []bccapi.VolumeAttachmentModel{
						{
							VolumeId:   "v-xxxx",
							InstanceId: "i-xxxx",
							Serial:     "v-xxxx",
							Device:     "/dev/vdc",
						},
					},
				}).AnyTimes()
				cdsVolume.EXPECT().IsDetaching().Return(false)
				cdsVolume.EXPECT().Detach(gomock.Any(), "i-xxxx").Return(nil)
				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)
				nodeService := cloudmock.NewMockNodeService(ctrl)
				nodeService.EXPECT().GetNodeByID(gomock.Any(), "i-xxxx", gomock.Any()).Return(nil, nil)
				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			req:          &normalReq,
			expectedResp: nil,
			expectedErr:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			resp, err := tc.mocks.server.ControllerUnpublishVolume(context.Background(), tc.req)
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

func TestControllerServer_ControllerExpandVolume(t *testing.T) {
	type mocks struct {
		ctrl   *gomock.Controller
		server csi.ControllerServer
	}

	from5to10ExpandReq := &csi.ControllerExpandVolumeRequest{
		VolumeId: "v-xxxx",
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: 10 * util.GB,
			LimitBytes:    10 * util.GB,
		},
		Secrets: map[string]string{
			common.AccessKey: "test-ak",
			common.SecretKey: "test-sk",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
		},
	}

	type args struct {
		ctx context.Context
		req *csi.ControllerExpandVolumeRequest
	}
	tests := []struct {
		name    string
		mocks   mocks
		args    args
		want    *csi.ControllerExpandVolumeResponse
		wantErr string
	}{
		// TODO: Add test cases.
		{
			name: "successfully trigger expansion from 5GB to 10GB",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(5),
					cdsVolume.EXPECT().IsAvailable().Return(true),
					cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
						StorageType: bccapi.StorageTypeHP1,
					}),
					cdsVolume.EXPECT().Resize(gomock.Any(), &cloud.ResizeCSDVolumeArgs{
						NewCdsSizeInGB: 10,
						NewVolumeType:  bccapi.StorageTypeHP1,
					}),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "volume is scaling", // error due to expansion is async operation
		},
		{
			name: "volume is scaling from 5GB to 10GB",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				cdsVolume.EXPECT().IsScaling().Return(true)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "volume is scaling", // error due to expansion is async operation
		},
		{
			name: "successfully complete expansion from 5GB to 10GB",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(10),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			want: &csi.ControllerExpandVolumeResponse{
				CapacityBytes:         util.SizeGBToBytes(10),
				NodeExpansionRequired: true,
			},
		},
		{
			name: "abort due to volume is attaching",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(5),
					cdsVolume.EXPECT().IsAvailable().Return(false),
					cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
						Status: bccapi.VolumeStatusATTACHING,
					}),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "Abort expansion for volume v-xxxx due to its status is Attaching",
		},
		{
			name: "abort due to volume size is larger than want size",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(12),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "must be no less than current size=12GB",
		},
		{
			name: "error occurs in GetVolumeByID",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(nil, fmt.Errorf("some error occurs in GetVolumeByID"))

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "some error occurs in GetVolumeByID",
		},
		{
			name: "online expansion is enabled and successfully trigger online expansion from 5GB to 10GB",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(5),
					cdsVolume.EXPECT().IsAvailable().Return(false),
					cdsVolume.EXPECT().IsInUse().Return(true),
					cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
						StorageType: bccapi.StorageTypeHP1,
					}),
					cdsVolume.EXPECT().Resize(gomock.Any(), &cloud.ResizeCSDVolumeArgs{
						NewCdsSizeInGB: 10,
						NewVolumeType:  bccapi.StorageTypeHP1,
					}),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{
					EnableOnlineExpansion: true,
				})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "volume is scaling", // error due to expansion is async operation
		},
		{
			name: "error occurs in cds resize request",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				cdsVolume := cloudmock.NewMockCDSVolume(ctrl)
				gomock.InOrder(
					cdsVolume.EXPECT().IsScaling().Return(false),
					cdsVolume.EXPECT().SizeGB().Return(5),
					cdsVolume.EXPECT().IsAvailable().Return(true),
					cdsVolume.EXPECT().Detail().Return(&bccapi.VolumeModel{
						StorageType: bccapi.StorageTypeHP1,
					}),
					cdsVolume.EXPECT().Resize(gomock.Any(), &cloud.ResizeCSDVolumeArgs{
						NewCdsSizeInGB: 10,
						NewVolumeType:  bccapi.StorageTypeHP1,
					}).Return(fmt.Errorf("some error occurs in Resize")),
				)

				volumeService := cloudmock.NewMockCDSVolumeService(ctrl)
				volumeService.EXPECT().GetVolumeByID(gomock.Any(), "v-xxxx", gomock.Any()).Return(cdsVolume, nil)

				nodeService := cloudmock.NewMockNodeService(ctrl)

				server := newControllerServer(volumeService, nodeService, &common.DriverOptions{})
				return mocks{
					ctrl:   ctrl,
					server: server,
				}
			}(),
			args: args{
				ctx: context.TODO(),
				req: from5to10ExpandReq,
			},
			wantErr: "some error occurs in Resize", // error due to expansion is async operation
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mocks.ctrl != nil {
				defer tt.mocks.ctrl.Finish()
			}
			got, err := tt.mocks.server.ControllerExpandVolume(tt.args.ctx, tt.args.req)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			}

			assert.DeepEqual(t, got, tt.want)
		})
	}
}
