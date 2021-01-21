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
	"flag"
	"fmt"
	"os"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
)

const (
	DriverTypeCDS = "cds"
	DriverTypeBOS = "bos"
)

const (
	defaultBosfsImage = "registry.baidubce.com/cce-plugin-pro/bosfs:1.0.0.10"
)

var (
	DriverVersion   string
	DriverGitCommit string
)

var (
	DriverTypeVar string

	CSIEndpointVar string
	ShowVersionVar bool

	ModeVar   string
	RegionVar string

	ClusterIDVar string
	AuthModeVar  string

	MaxVolumesPerNodeVar int

	BCCEndpointVar string
	CDSEndpointVar string
	BOSEndpointVar string

	BosfsImageVar string
)

type Options struct {
	DriverType        string
	CSIEndpoint       string
	Mode              common.DriverMode
	Region            string
	ClusterID         string
	AuthMode          cloud.AuthMode
	NodeID            string
	Zone              string
	MaxVolumesPerNode int
	BCCEndpoint       string
	CDSEndpoint       string
	BOSEndpoint       string
	BosfsImage        string
}

func init() {
	flag.StringVar(&DriverTypeVar, "driver-type", "", "CSI driver type. Supported: `cds` and `bos`.")
	flag.StringVar(&CSIEndpointVar, "csi-endpoint", "", "CSI driver endpoint.")
	flag.StringVar(&ModeVar, "driver-mode", "all", "CSI driver mode, supported: 'controller', 'node' and 'all'. Default 'all'.")
	flag.StringVar(&RegionVar, "region", "", "Default region of node in the cluster. (optional)")
	flag.StringVar(&ClusterIDVar, "cluster-id", "", "Cluster ID of the cluster where driver controller server will run in. (optional)")
	flag.StringVar(&AuthModeVar, "auth-mode", "key", "Auth mode of driver, supported: 'key' and 'gateway'. Default 'key'.")
	flag.IntVar(&MaxVolumesPerNodeVar, "max-volumes-per-node", 0, "The limit of max volumes per node. Zero means no limit. (optional)")
	flag.StringVar(&BCCEndpointVar, "bcc-endpoint", "", "Endpoint of BCC openapi service. (optional)")
	flag.StringVar(&CDSEndpointVar, "cds-endpoint", "", "Endpoint of CDS openapi service. (optional)")
	flag.StringVar(&BOSEndpointVar, "bos-endpoint", "", "Endpoint of BOS openapi service. (optional)")
	flag.StringVar(&BosfsImageVar, "bosfs-image", defaultBosfsImage, "bosfs image use by CSI bosplugin. (optional)")

	flag.BoolVar(&ShowVersionVar, "version", false, "Show CSI driver version.")
}

func ParseFlags() (*Options, error) {
	flag.Parse()
	cloud.InitLog()
	if ShowVersionVar {
		fmt.Println(os.Args[0], fmt.Sprintf("version: %s, git commitID: %s", DriverVersion, DriverGitCommit))
		os.Exit(0)
	}

	var options Options
	options.CSIEndpoint = CSIEndpointVar
	options.Region = RegionVar
	options.ClusterID = ClusterIDVar
	options.MaxVolumesPerNode = MaxVolumesPerNodeVar

	switch DriverTypeVar {
	case DriverTypeCDS:
		options.DriverType = DriverTypeCDS
		return parseCDSDriverFlags(&options)
	case DriverTypeBOS:
		options.DriverType = DriverTypeBOS
		return parseBOSDriverFlags(&options)
	default:
		return &options, nil
	}
}

func parseBOSDriverFlags(options *Options) (*Options, error) {
	switch common.DriverMode(ModeVar) {
	case common.DriverModeController:
		return nil, fmt.Errorf("bosplugin does not support driver-mode: controller")
	case common.DriverModeNode:
		options.Mode = common.DriverModeNode
	case common.DriverModeAll:
		options.Mode = common.DriverModeAll
	default:
		return nil, fmt.Errorf("invalid driver mode: %s", ModeVar)
	}

	switch cloud.AuthMode(AuthModeVar) {
	case cloud.AuthModeAccessKey:
		options.AuthMode = cloud.AuthModeAccessKey
	case cloud.AuthModeCCEGateway:
		return nil, fmt.Errorf("bosplugin does not support auth-mode: gateway")
	default:
		return nil, fmt.Errorf("invalid auth mode: %s", AuthModeVar)
	}

	options.BOSEndpoint = BOSEndpointVar
	if options.BOSEndpoint == "" {
		if options.Region == "" {
			return nil, fmt.Errorf("region or bos-endpoint must be providered")
		}

		// https://cloud.baidu.com/doc/BOS/s/Ck1rk80hn
		options.BOSEndpoint = options.Region + ".bcebos.com"
	}

	options.BosfsImage = BosfsImageVar

	return options, nil
}

func parseCDSDriverFlags(options *Options) (*Options, error) {
	switch common.DriverMode(ModeVar) {
	case common.DriverModeController:
		options.Mode = common.DriverModeController
	case common.DriverModeNode:
		options.Mode = common.DriverModeNode
	case common.DriverModeAll:
		options.Mode = common.DriverModeAll
	default:
		return nil, fmt.Errorf("invalid driver mode: %s", ModeVar)
	}

	switch cloud.AuthMode(AuthModeVar) {
	case cloud.AuthModeAccessKey:
		options.AuthMode = cloud.AuthModeAccessKey
	case cloud.AuthModeCCEGateway:
		options.AuthMode = cloud.AuthModeCCEGateway
	default:
		return nil, fmt.Errorf("invalid auth mode: %s", AuthModeVar)
	}

	options.BCCEndpoint = BCCEndpointVar
	if options.BCCEndpoint == "" {
		if options.Region == "" {
			return nil, fmt.Errorf("region or bcc-endpoint must be providered")
		}

		// https://cloud.baidu.com/doc/BCC/s/0jwvyo603
		options.BCCEndpoint = "bcc." + options.Region + ".baidubce.com"
	}

	options.CDSEndpoint = CDSEndpointVar
	if options.CDSEndpoint == "" {
		if options.Region == "" {
			return nil, fmt.Errorf("region or cds-endpoint must be providered")
		}

		// https://cloud.baidu.com/doc/BCC/s/0jwvyo603
		options.CDSEndpoint = "bcc." + options.Region + ".baidubce.com"
	}

	return options, nil
}
