/*
 * Copyright 2021 Baidu, Inc.
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

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud"
)

const (
	metaServiceTopologyMode TopologyMode = "meta"
	k8sNodeTopologyMode     TopologyMode = "k8s"
	autoTopologyMode        TopologyMode = "auto"

	nodeNameENVName     = "NODE_NAME"
	cceProviderIDPrefix = "cce://"
)

type TopologyMode string

// Get node id and zone
func GetNodeTopology(ctx context.Context, mode TopologyMode) (string, string, error) {
	switch mode {
	case metaServiceTopologyMode:
		nodeID, zone, metaServiceErr := getNodeTopologyFromMetaService(ctx)
		if metaServiceErr != nil {
			glog.Errorf("Failed to get topology from meta service, err: %v, try to get it from k8s node", metaServiceErr)
			return "", "", metaServiceErr
		}
		glog.V(4).Infof("Succeed to get topology from meta service, nodeID: %s, zone: %s", nodeID, zone)
		return nodeID, zone, nil
	case k8sNodeTopologyMode:
		nodeID, zone, k8sNodeErr := getNodeTopologyFromK8SNode(ctx)
		if k8sNodeErr != nil {
			glog.Errorf("Failed to get topology from k8s node, err: %v", k8sNodeErr)
			return "", "", k8sNodeErr
		}
		glog.V(4).Infof("Succeed to get topology from k8s node, nodeID: %s, zone: %s", nodeID, zone)
		return nodeID, zone, nil
	case autoTopologyMode:
		nodeID, zone, metaServiceErr := getNodeTopologyFromMetaService(ctx)
		if metaServiceErr == nil {
			glog.V(4).Infof("Succeed to get topology from meta service, nodeID: %s, zone: %s", nodeID, zone)
			return nodeID, zone, nil
		}
		glog.Warningf("Failed to get topology from meta service, err: %v, try to get it from k8s node", metaServiceErr)

		nodeID, zone, k8sNodeErr := getNodeTopologyFromK8SNode(ctx)
		if k8sNodeErr == nil {
			glog.V(4).Infof("Succeed to get topology from k8s node, nodeID: %s, zone: %s", nodeID, zone)
			return nodeID, zone, nil
		}
		glog.Warningf("Failed to get topology from k8s node, err: %v", k8sNodeErr)

		return "", "", fmt.Errorf("failed to get topology from meta service or k8s node, errs: %s, %s", metaServiceErr, k8sNodeErr)
	default:
		return "", "", fmt.Errorf("invalid topology mode: %s", mode)
	}
}

func getNodeTopologyFromMetaService(ctx context.Context) (string, string, error) {
	metaService, err := cloud.NewMetaDataService()
	if err != nil {
		return "", "", fmt.Errorf("failed to get meta data, err: %v", err)
	}
	return metaService.InstanceID(), metaService.Zone(), nil
}

func getNodeTopologyFromK8SNode(ctx context.Context) (string, string, error) {
	nodeName := os.Getenv(nodeNameENVName)
	if nodeName == "" {
		return "", "", fmt.Errorf("env NODE_NAME is not set")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return "", "", err
	}

	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", err
	}

	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}

	if node.Spec.ProviderID == "" {
		return "", "", fmt.Errorf("providerID is not set in node spec")
	}

	if !strings.HasPrefix(node.Spec.ProviderID, cceProviderIDPrefix) {
		return "", "", fmt.Errorf("known providerID format: %s", node.Spec.ProviderID)
	}

	nodeID := node.Spec.ProviderID[len(cceProviderIDPrefix):]

	zone, found := node.Labels[corev1.LabelFailureDomainBetaZone]
	if !found {
		return "", "", fmt.Errorf("zone is not set in node labels")
	}

	return nodeID, zone, nil
}
