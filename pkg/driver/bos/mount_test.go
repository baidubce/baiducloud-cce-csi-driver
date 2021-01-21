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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/golang/mock/gomock"
	"k8s.io/utils/mount"

	bosmock "github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/bos/mock"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

func TestBosfsMounter_MountByBOSFS(t *testing.T) {
	type mocks struct {
		ctrl    *gomock.Controller
		mounter Mounter
	}

	type argv struct {
		ctx              context.Context
		source           string
		target           string
		options          []string
		sensitiveOptions []string
	}

	normalArgv := argv{
		ctx:    context.WithValue(context.Background(), util.TraceIDKey, "test"),
		source: "test-source",
		target: "/test/target",
	}

	normalCreateContainerConfig := containertypes.Config{
		Entrypoint: []string{
			"/bin/bash",
		},
		Cmd: []string{
			"-c",
			"(umount /test/target || echo ignore error > /dev/null) && bosfs test-source /test/target -f",
		},
		Image: "bosfs",
		Healthcheck: &containertypes.HealthConfig{
			Test: []string{
				"CMD-SHELL",
				fmt.Sprintf("stat %s || exit 1", "/test/target"),
			},
			Interval:    5 * time.Second,
			Timeout:     3 * time.Second,
			StartPeriod: 400 * time.Millisecond,
			Retries:     3,
		},
	}

	normalCreateContainerHostConfig := containertypes.HostConfig{
		Mounts: []mounttypes.Mount{
			{
				Type:   mounttypes.TypeBind,
				Source: path.Dir("/test/target"),
				Target: path.Dir("/test/target"),
				BindOptions: &mounttypes.BindOptions{
					Propagation: mounttypes.PropagationRShared,
				},
			},
			{
				Type:   mounttypes.TypeBind,
				Source: "/dev/fuse",
				Target: "/dev/fuse",
			},
			{
				Type:   mounttypes.TypeBind,
				Source: path.Dir(getCredentialsFilePath("/test/target")),
				Target: path.Dir(getCredentialsFilePath("/test/target")),
			},
		},
		RestartPolicy: containertypes.RestartPolicy{
			Name: "unless-stopped",
		},
		LogConfig: containertypes.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-file": "5",
				"max-size": "40m",
			},
		},
		NetworkMode: "host",
		Privileged:  true,
		CapAdd: []string{
			"SYS_ADMIN",
		},
	}

	testCases := []struct {
		name  string
		mocks mocks
		argv
		expectedErr bool
	}{
		{
			name: "get bosfs container failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{}, fmt.Errorf("test"))
				mounter := &bosfsMounter{
					containerClient: containerClient,
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "bosfs container not exist and start pull image failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				imageClient := bosmock.NewMockImageAPIClient(ctrl)
				imageClient.EXPECT().ImagePull(gomock.Any(), "bosfs", gomock.Any()).Return(nil, fmt.Errorf("test"))

				mounter := &bosfsMounter{
					containerClient: containerClient,
					imageClient:     imageClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "bosfs container not exist and pull image failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				imageClient := bosmock.NewMockImageAPIClient(ctrl)

				errMsg := jsonmessage.JSONMessage{
					Error: &jsonmessage.JSONError{
						Code:    1,
						Message: "test",
					},
				}
				imageClient.EXPECT().ImagePull(gomock.Any(), "bosfs", gomock.Any()).Return(ioutil.NopCloser(bytes.NewBuffer([]byte(util.ToJSONOrPanic(errMsg)))), nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					imageClient:     imageClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "create bosfs container failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				containerClient.EXPECT().ContainerCreate(gomock.Any(), gomock.Eq(&normalCreateContainerConfig), gomock.Eq(&normalCreateContainerHostConfig), nil, nil, getContainerName("/test/target")).Return(containertypes.ContainerCreateCreatedBody{}, fmt.Errorf("test"))
				imageClient := bosmock.NewMockImageAPIClient(ctrl)
				imageClient.EXPECT().ImagePull(gomock.Any(), "bosfs", gomock.Any()).Return(ioutil.NopCloser(bytes.NewBuffer(nil)), nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					imageClient:     imageClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "create bosfs container failed",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				containerClient.EXPECT().ContainerCreate(gomock.Any(), gomock.Eq(&normalCreateContainerConfig), gomock.Eq(&normalCreateContainerHostConfig), nil, nil, getContainerName("/test/target")).Return(containertypes.ContainerCreateCreatedBody{}, fmt.Errorf("test"))
				imageClient := bosmock.NewMockImageAPIClient(ctrl)
				imageClient.EXPECT().ImagePull(gomock.Any(), "bosfs", gomock.Any()).Return(ioutil.NopCloser(bytes.NewBuffer(nil)), nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					imageClient:     imageClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "bosfs container is dead",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "dead",
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerRemove(gomock.Any(), "test-id", gomock.Eq(types.ContainerRemoveOptions{Force: true})).Return(nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "bosfs container is running and healthy",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "healthy",
							},
						},
					},
				}, nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: false,
		},
		{
			name: "bosfs container is running and starting",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "starting",
							},
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), "test-id").Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "healthy",
							},
						},
					},
				}, nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: false,
		},
		{
			name: "bosfs container is restarting",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "restarting",
						},
					},
				}, nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
		{
			name: "bosfs container is created",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "created",
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerStart(gomock.Any(), "test-id", gomock.Any()).Return(nil)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), "test-id").Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "healthy",
							},
						},
					},
				}, nil)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: false,
		},
		{
			name: "bosfs container is running but failed to wait for ready",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName(normalArgv.target)).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "starting",
							},
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), "test-id").Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
							Health: &types.Health{
								Status: "unhealthy",
							},
						},
					},
				}, nil).MinTimes(1)

				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			argv:        normalArgv,
			expectedErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			err := tc.mocks.mounter.MountByBOSFS(tc.argv.ctx, tc.argv.source, tc.argv.target, tc.argv.options, tc.argv.sensitiveOptions)
			if (err == nil && tc.expectedErr) || (err != nil && !tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
			}
		})
	}
}

func TestBosfsMounter_UnmountFromBOSFS(t *testing.T) {
	type mocks struct {
		ctrl    *gomock.Controller
		mounter Mounter
	}

	testCases := []struct {
		name        string
		mocks       mocks
		target      string
		expectedErr bool
	}{
		{
			name: "failed to get container",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{}, fmt.Errorf("test"))
				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: true,
		},
		{
			name: "failed to stop running container",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerStop(gomock.Any(), "test-id", nil).Return(fmt.Errorf("test"))
				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: true,
		},
		{
			name: "failed to remove created container",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "created",
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerRemove(gomock.Any(), "test-id", types.ContainerRemoveOptions{}).Return(fmt.Errorf("test"))
				mounter := &bosfsMounter{
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: true,
		},
		{
			name: "bosfs container not exists and target is not mount point",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				m := bosmock.NewMockInterface(ctrl)
				m.EXPECT().List().Return(nil, nil)
				mounter := &bosfsMounter{
					Interface:       m,
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: false,
		},
		{
			name: "bosfs container not exists but target is mount point",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{}, errdefs.NotFound(fmt.Errorf("test")))
				m := bosmock.NewMockInterface(ctrl)
				m.EXPECT().List().Return([]mount.MountPoint{
					{
						Path: "/test/target",
					},
				}, nil)
				m.EXPECT().Unmount("/test/target").Return(nil)
				mounter := &bosfsMounter{
					Interface:       m,
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: false,
		},
		{
			name: "bosfs container is running",
			mocks: func() mocks {
				ctrl := gomock.NewController(t)
				containerClient := bosmock.NewMockContainerAPIClient(ctrl)
				containerClient.EXPECT().ContainerInspect(gomock.Any(), getContainerName("/test/target")).Return(types.ContainerJSON{
					ContainerJSONBase: &types.ContainerJSONBase{
						ID: "test-id",
						State: &types.ContainerState{
							Status: "running",
						},
					},
				}, nil)
				containerClient.EXPECT().ContainerStop(gomock.Any(), "test-id", nil).Return(nil)
				containerClient.EXPECT().ContainerRemove(gomock.Any(), "test-id", types.ContainerRemoveOptions{}).Return(nil)
				m := bosmock.NewMockInterface(ctrl)
				m.EXPECT().List().Return([]mount.MountPoint{
					{
						Path: "/test/target",
					},
				}, nil)
				m.EXPECT().Unmount("/test/target").Return(nil)
				mounter := &bosfsMounter{
					Interface:       m,
					containerClient: containerClient,
					bosfsImage:      "bosfs",
				}
				return mocks{
					ctrl:    ctrl,
					mounter: mounter,
				}
			}(),
			target:      "/test/target",
			expectedErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer tc.mocks.ctrl.Finish()
			err := tc.mocks.mounter.UnmountFromBOSFS(context.Background(), tc.target)
			if (err == nil && tc.expectedErr) || (err != nil && !tc.expectedErr) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
			}
		})
	}
}
