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
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
)

type Driver struct {
	csi.ControllerServer
	csi.NodeServer

	options *common.DriverOptions
}

func NewDriver(setOptions ...func(*common.DriverOptions)) (*Driver, error) {
	var options common.DriverOptions
	for _, setOption := range setOptions {
		setOption(&options)
	}

	var controller csi.ControllerServer
	var node csi.NodeServer

	if options.Mode == common.DriverModeController || options.Mode == common.DriverModeAll {
		cdsService, err := cloud.NewCDSService(options.CDSEndpoint, options.Region)
		if err != nil {
			return nil, err
		}
		bccService, err := cloud.NewBCCService(options.BCCEndpoint, options.Region)
		if err != nil {
			return nil, err
		}
		controller = newControllerServer(cdsService, bccService, &options)
	}

	if options.Mode == common.DriverModeNode || options.Mode == common.DriverModeAll {
		if options.NodeOptions.NodeID == "" || options.NodeOptions.Zone == "" {
			metaService, err := cloud.NewMetaDataService()
			if err != nil {
				return nil, fmt.Errorf("failed to get meta data, err: %v", err)
			}
			options.NodeOptions.NodeID = metaService.InstanceID()
			options.NodeOptions.Zone = metaService.Zone()
		}
		node = newNodeServer(newMounter(), &options)
	}

	return &Driver{
		ControllerServer: controller,
		NodeServer:       node,
		options:          &options,
	}, nil
}

func (d *Driver) Run(endpoint string) {
	grpcServer := common.NewGRPCServer()
	grpcServer.Start(endpoint, d, d.ControllerServer, d.NodeServer)
}
