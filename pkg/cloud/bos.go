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
	"errors"
	"fmt"
	"net/http"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bos"
)

//go:generate mockgen -destination=./mock/mock_bos_service.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud BOSService
type BOSService interface {
	BucketExists(ctx context.Context, bucketName string, auth Auth) (bool, error)
}

type bosService struct {
	client func(ctx context.Context, auth Auth) *bos.Client
}

func NewBOSService(endpoint string) (BOSService, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}

	return &bosService{
		client: func(ctx context.Context, auth Auth) *bos.Client {
			cred := auth.GetCredentials(ctx)
			client, _ := bos.NewClient(cred.AccessKeyId, cred.SecretAccessKey, endpoint)
			// overwrite credential if session token set.
			if cred.SessionToken != "" {
				client.Config.Credentials = cred
			}
			return client
		},
	}, nil
}

func (service *bosService) BucketExists(ctx context.Context, bucketName string, auth Auth) (bool, error) {
	client := service.client(ctx, auth)
	// NOTE: client.DoesBucketExist is not suitable here since it returns [true, nil] while 403 happens.
	err := client.HeadBucket(bucketName)
	if err == nil {
		return true, nil
	}
	var bceErr *bce.BceServiceError
	if errors.As(err, &bceErr) {
		if bceErr.StatusCode == http.StatusNotFound {
			return false, nil
		}
	}
	return false, err
}
