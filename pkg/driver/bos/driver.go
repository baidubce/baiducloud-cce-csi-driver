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

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/docker/client"
	"github.com/golang/glog"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
)

type Driver struct {
	csi.NodeServer

	options *common.DriverOptions
}

func NewDriver(setOptions ...func(*common.DriverOptions)) (*Driver, error) {
	var options common.DriverOptions
	for _, setOption := range setOptions {
		setOption(&options)
	}

	var node csi.NodeServer

	if options.Mode == common.DriverModeController {
		return nil, fmt.Errorf("controller server is not supported")
	}

	if options.Mode == common.DriverModeNode || options.Mode == common.DriverModeAll {
		if options.NodeOptions.NodeID == "" || options.NodeOptions.Zone == "" {
			nodeID, zone, err := common.GetNodeTopology(context.Background(), options.TopologyMode)
			if err != nil {
				return nil, fmt.Errorf("failed to get nodeID or zone, err: %v", err)
			}

			options.NodeOptions.NodeID = nodeID
			options.NodeOptions.Zone = zone
		}

		bosService, err := cloud.NewBOSService(options.BOSEndpoint)
		if err != nil {
			return nil, err
		}

		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			glog.Errorf("Failed to create docker client, err: %v", err)
			return nil, err
		}

		node = newNodeServer(bosService, newBOSFSMounter(options.BosfsImage, cli, cli), &options)
	}

	return &Driver{
		NodeServer: node,
		options:    &options,
	}, nil
}

func (d *Driver) Run(endpoint string) {
	grpcServer := common.NewGRPCServer()
	grpcServer.Start(endpoint, d, &csi.UnimplementedControllerServer{}, d.NodeServer)
}
