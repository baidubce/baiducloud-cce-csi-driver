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

package cloud

import (
	"context"
	"fmt"

	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/golang/glog"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

type bccService struct {
	client func(ctx context.Context, auth Auth) *bcc.Client
}

func NewBCCService(endpoint, region string) (NodeService, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}

	return &bccService{
		client: func(ctx context.Context, auth Auth) *bcc.Client {
			return generateBCCCDSClient(ctx, endpoint, region, auth)
		},
	}, nil
}

func (service *bccService) GetNodeByID(ctx context.Context, nodeID string, auth Auth) (Node, error) {
	result, err := service.client(ctx, auth).GetInstanceDetail(nodeID)
	if err != nil {
		return nil, err
	}

	glog.V(5).Infof("[%s] GetInstanceDetail: id: %s, result: %s", ctx.Value(util.TraceIDKey), nodeID, util.ToJSONOrPanic(result))
	return &bccNode{
		InstanceModel: result.Instance,
	}, nil
}

type bccNode struct {
	bccapi.InstanceModel
}
