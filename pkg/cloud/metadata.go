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
	"io/ioutil"
	"net/http"
)

const (
	bccMetaDataInstanceShortIDEndpoint = "http://169.254.169.254/2009-04-04/meta-data/instance-shortid"
	bccMetaDataAZoneEndpoint           = "http://169.254.169.254/2009-04-04/meta-data/azone"
)

//go:generate mockgen -destination=./mock/mock_metadata.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud MetaDataService

// MetaDataService represents BaiduCloud BCC meta data service.
type MetaDataService interface {
	// InstanceID returns instance id of which the instance is in.
	InstanceID() string
	// Zone returns zone of which the instance is in.
	Zone() string
}

type metaDataService struct {
	metaData metaData
}

type metaData struct {
	instanceID string
	zone       string
}

func (service *metaDataService) InstanceID() string {
	return service.metaData.instanceID
}

func (service *metaDataService) Zone() string {
	return service.metaData.zone
}

// NewMetaDataService returns a new MetaDataService implementation.
func NewMetaDataService() (MetaDataService, error) {
	metaData := metaData{}

	instanceIDResp, err := http.DefaultClient.Get(bccMetaDataInstanceShortIDEndpoint)
	if err != nil {
		return nil, err
	}

	defer instanceIDResp.Body.Close()
	instanceID, err := ioutil.ReadAll(instanceIDResp.Body)
	if err != nil {
		return nil, err
	}
	metaData.instanceID = string(instanceID)

	zoneResp, err := http.DefaultClient.Get(bccMetaDataAZoneEndpoint)
	if err != nil {
		return nil, err
	}

	defer zoneResp.Body.Close()
	zone, err := ioutil.ReadAll(zoneResp.Body)
	if err != nil {
		return nil, err
	}
	metaData.zone = string(zone)

	return &metaDataService{
		metaData: metaData,
	}, nil
}
