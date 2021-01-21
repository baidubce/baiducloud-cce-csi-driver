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

package common

import "github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"

const (
	DriverModeController DriverMode = "controller"
	DriverModeNode       DriverMode = "node"
	DriverModeAll        DriverMode = "all"
)

type DriverMode string

type DriverOptions struct {
	ControllerOptions
	NodeOptions
	EndpointOptions
	ImageOptions
	DriverVersion string
	Mode          DriverMode
	AuthMode      cloud.AuthMode
	Region        string
}

type ControllerOptions struct {
	ClusterID string
}

type NodeOptions struct {
	NodeID            string
	Zone              string
	MaxVolumesPerNode int
}

type EndpointOptions struct {
	BCCEndpoint string
	CDSEndpoint string
	BOSEndpoint string
}

type ImageOptions struct {
	BosfsImage string
}

func WithVersion(version string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.DriverVersion = version
	}
}

func WithMode(mode DriverMode) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.Mode = mode
	}
}

func WithRegion(region string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.Region = region
	}
}

func WithClusterID(clusterID string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.ClusterID = clusterID
	}
}

func WithAuthMode(mode cloud.AuthMode) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.AuthMode = mode
	}
}

func WithNodeID(nodeID string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.NodeID = nodeID
	}
}

func WithZone(zone string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.Zone = zone
	}
}

func WithMaxVolumesPerNode(maxVolumesPerNode int) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.MaxVolumesPerNode = maxVolumesPerNode
	}
}

func WithBCCEndpoint(endpoint string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.BCCEndpoint = endpoint
	}
}

func WithCDSEndpoint(endpoint string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.CDSEndpoint = endpoint
	}
}

func WithBOSEndpoint(endpoint string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.BOSEndpoint = endpoint
	}
}

func WithBosfsImage(image string) func(options *DriverOptions) {
	return func(options *DriverOptions) {
		options.BosfsImage = image
	}
}
