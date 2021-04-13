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

package main

import (
	"os"

	"github.com/golang/glog"

	bosdriver "github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/bos"
	cdsdriver "github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/cds"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
)

func main() {
	options, err := ParseFlags()
	if err != nil {
		glog.Errorf("Invalid options, %v", err)
		os.Exit(1)
	}

	switch options.DriverType {
	case DriverTypeCDS:
		driver, err := cdsdriver.NewDriver(
			common.WithVersion(DriverVersion),
			common.WithMode(options.Mode),
			common.WithRegion(options.Region),
			common.WithClusterID(options.ClusterID),
			common.WithAuthMode(options.AuthMode),
			common.WithBCCEndpoint(options.BCCEndpoint),
			common.WithCDSEndpoint(options.CDSEndpoint),
			common.WithMaxVolumesPerNode(options.MaxVolumesPerNode),
			common.WithTopologyMode(options.TopologyMode),
			common.WithOverrideDriverName(options.OverrideDriverName),
		)
		if err != nil {
			glog.Errorf("Failed to init CSI driver, err: %v", err)
			os.Exit(1)
		}

		driver.Run(options.CSIEndpoint)
	case DriverTypeBOS:
		driver, err := bosdriver.NewDriver(
			common.WithVersion(DriverVersion),
			common.WithMode(options.Mode),
			common.WithRegion(options.Region),
			common.WithClusterID(options.ClusterID),
			common.WithAuthMode(options.AuthMode),
			common.WithBOSEndpoint(options.BOSEndpoint),
			common.WithMaxVolumesPerNode(options.MaxVolumesPerNode),
			common.WithBosfsImage(options.BosfsImage),
			common.WithTopologyMode(options.TopologyMode),
			common.WithOverrideDriverName(options.OverrideDriverName),
		)
		if err != nil {
			glog.Errorf("Failed to init CSI driver, err: %v", err)
			os.Exit(1)
		}

		driver.Run(options.CSIEndpoint)
	default:
		glog.Errorf("Unsupported driver type: %s", options.DriverType)
		os.Exit(1)
	}
}
