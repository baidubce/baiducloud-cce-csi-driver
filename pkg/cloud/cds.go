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
	"net/http"
	"strings"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bcc"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
	"github.com/golang/glog"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

//go:generate mockgen -destination=./mock/mock_volume_service.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud CDSVolumeService

type CDSVolumeService interface {
	CreateVolume(ctx context.Context, args *CreateCDSVolumeArgs, auth Auth) (string, error)
	GetVolumeByID(ctx context.Context, id string, auth Auth) (CDSVolume, error)
	GetVolumeByName(ctx context.Context, name string, auth Auth) (CDSVolume, error)
}

//go:generate mockgen -destination=./mock/mock_cds_volume.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/cloud CDSVolume

type CDSVolume interface {
	Delete(ctx context.Context) error
	Attach(ctx context.Context, instanceID string) (*bccapi.VolumeAttachmentModel, error)
	Detach(ctx context.Context, instanceID string) error
	Resize(ctx context.Context, args *ResizeCSDVolumeArgs) error

	ID() string
	Zone() string
	SizeGB() int
	Detail() *bccapi.VolumeModel
	IsAvailable() bool
	IsInUse() bool
	IsScaling() bool
	IsAttached() bool
	IsAttaching() bool
	IsDetaching() bool
}

type CreateCDSVolumeArgs struct {
	Name                string
	Description         string
	SnapshotID          string
	ZoneName            string
	CdsSizeInGB         int
	StorageType         string
	EncryptKey          string
	ClientToken         string
	PaymentTiming       string
	ReservationLength   int
	ReservationTimeUnit string
	Tags                map[string]string
}

type ResizeCSDVolumeArgs struct {
	NewCdsSizeInGB int
	NewVolumeType  bccapi.StorageType
}

type Billing struct {
	PaymentTiming string
	Reservation   *Reservation
}

type Reservation struct {
	ReservationLength   int
	ReservationTimeUnit string
}

// cdsService implement CDSVolumeService interface.
type cdsService struct {
	client func(ctx context.Context, auth Auth) *bcc.Client
}

func NewCDSService(endpoint, region string) (CDSVolumeService, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint")
	}

	return &cdsService{
		client: func(ctx context.Context, auth Auth) *bcc.Client {
			return generateBCCCDSClient(ctx, endpoint, region, auth)
		},
	}, nil
}

func (service *cdsService) CreateVolume(ctx context.Context, args *CreateCDSVolumeArgs, auth Auth) (string, error) {

	reqArgs := &bccapi.CreateCDSVolumeArgs{
		Name:          args.Name,
		Description:   args.Description,
		SnapshotId:    args.SnapshotID,
		ZoneName:      args.ZoneName,
		PurchaseCount: 1,
		CdsSizeInGB:   args.CdsSizeInGB,
		StorageType:   bccapi.StorageType(args.StorageType),
		Billing: &bccapi.Billing{
			PaymentTiming: bccapi.PaymentTimingType(args.PaymentTiming),
			Reservation:   nil,
		},
		EncryptKey:  args.EncryptKey,
		ClientToken: args.ClientToken,
	}

	if reqArgs.Billing.PaymentTiming == bccapi.PaymentTimingPrePaid {
		reqArgs.Billing.Reservation = &bccapi.Reservation{
			ReservationLength:   args.ReservationLength,
			ReservationTimeUnit: args.ReservationTimeUnit,
		}
	}

	result, err := service.client(ctx, auth).CreateCDSVolume(reqArgs)
	if err != nil {
		return "", err
	}
	if len(result.VolumeIds) != 1 {
		glog.Errorf("[%s] Request to create 1 cds, but got %d, volume ids: %v", ctx.Value(util.TraceIDKey), len(result.VolumeIds), result.VolumeIds)
		return "", fmt.Errorf("invalid volume ids size got from creating service")
	}
	return result.VolumeIds[0], nil
}

func (service *cdsService) GetVolumeByID(ctx context.Context, id string, auth Auth) (CDSVolume, error) {
	client := service.client(ctx, auth)
	result, err := client.GetCDSVolumeDetail(id)
	if err != nil {
		return nil, err
	}
	glog.V(5).Infof("[%s] GetCDSVolumeDetail: id: %s, result: %s", ctx.Value(util.TraceIDKey), id, util.ToJSONOrPanic(result))

	return &cdsVolume{
		VolumeModel: *result.Volume,
		client:      client,
	}, nil
}

func (service *cdsService) GetVolumeByName(ctx context.Context, name string, auth Auth) (CDSVolume, error) {
	client := service.client(ctx, auth)

	maker := ""
	var volumes []bccapi.VolumeModel
	for {
		args := &bccapi.ListCDSVolumeArgs{
			MaxKeys: 100,
			Marker:  maker,
		}
		result, err := client.ListCDSVolume(args)
		if err != nil {
			return nil, err
		}

		for _, volume := range result.Volumes {
			if volume.Name == name {
				volumes = append(volumes, volume)
			}
		}
		if !result.IsTruncated {
			break
		}
		maker = result.NextMarker
	}

	glog.V(5).Infof("[%s] ListCDSVolume: name: %s, result: %s", ctx.Value(util.TraceIDKey), name, util.ToJSONOrPanic(volumes))
	if len(volumes) == 0 {
		return nil, &bce.BceServiceError{StatusCode: http.StatusNotFound}
	}
	if len(volumes) > 1 {
		return nil, ErrMultiVolumes
	}
	return &cdsVolume{
		VolumeModel: volumes[0],
		client:      client,
	}, nil
}

type cdsVolume struct {
	bccapi.VolumeModel

	client *bcc.Client
}

func (v *cdsVolume) Delete(ctx context.Context) error {
	if v.Id == "" {
		return fmt.Errorf("empty volume id")
	}

	return v.client.DeleteCDSVolume(v.Id)
}

func (v *cdsVolume) Attach(ctx context.Context, instanceID string) (*bccapi.VolumeAttachmentModel, error) {
	if v.Id == "" {
		return nil, fmt.Errorf("empty volume id")
	}

	if instanceID == "" {
		return nil, fmt.Errorf("empty instance id")
	}

	args := &bccapi.AttachVolumeArgs{InstanceId: instanceID}
	result, err := v.client.AttachCDSVolume(v.Id, args)
	if err != nil {
		return nil, err
	}
	return result.VolumeAttachment, nil
}

func (v *cdsVolume) Detach(ctx context.Context, instanceID string) error {
	if v.Id == "" {
		return fmt.Errorf("empty volume id")
	}

	if instanceID == "" {
		return fmt.Errorf("empty instance id")
	}

	args := &bccapi.DetachVolumeArgs{InstanceId: instanceID}
	return v.client.DetachCDSVolume(v.Id, args)
}

func (v *cdsVolume) Resize(ctx context.Context, args *ResizeCSDVolumeArgs) error {
	if v.Id == "" {
		return fmt.Errorf("empty volume id")
	}

	reqArgs := &bccapi.ResizeCSDVolumeArgs{
		NewCdsSizeInGB: args.NewCdsSizeInGB,
		NewVolumeType:  args.NewVolumeType,
		// TODO: if client token is necessary
	}
	return v.client.ResizeCDSVolume(v.Id, reqArgs)
}

func (v *cdsVolume) ID() string {
	return v.Id
}

func (v *cdsVolume) Zone() string {
	// e.g. cn-bj-a => zoneA
	return "zone" + strings.ToUpper(v.ZoneName[strings.LastIndex(v.ZoneName, "-")+1:])
}

func (v *cdsVolume) SizeGB() int {
	return v.DiskSizeInGB
}

func (v *cdsVolume) Detail() *bccapi.VolumeModel {
	return &v.VolumeModel
}

func (v *cdsVolume) IsInUse() bool {
	return v.Status == bccapi.VolumeStatusINUSE
}

func (v *cdsVolume) IsScaling() bool {
	return v.Status == bccapi.VolumeStatusSCALING
}

func (v *cdsVolume) IsAvailable() bool {
	return v.Status == bccapi.VolumeStatusAVAILABLE
}

func (v *cdsVolume) IsAttached() bool {
	return v.Status == bccapi.VolumeStatusINUSE
}

func (v *cdsVolume) IsAttaching() bool {
	return v.Status == bccapi.VolumeStatusATTACHING
}

func (v *cdsVolume) IsDetaching() bool {
	return v.Status == bccapi.VolumeStatusDETACHING
}

func generateBCCCDSClient(ctx context.Context, endpoint, region string, auth Auth) *bcc.Client {
	defaultConf := &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    region,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               auth.GetCredentials(ctx),
		SignOption:                auth.GetSignOptions(ctx),
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: bce.DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS,
	}
	return &bcc.Client{BceClient: bce.NewBceClient(defaultConf, auth.GetSigner(ctx))}
}
