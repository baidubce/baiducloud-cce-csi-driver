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
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	mounttypes "github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/golang/glog"
	"k8s.io/utils/mount"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

const (
	credentialsFileContentTmpl = "bce_access_key_id=%s\nbce_secret_access_key=%s\nbce_security_token=%s\nreload_interval_s=1"
	credentialsFileDir         = "/etc/bosfs/credentials"
	bosfsTmpDir                = "/tmp"

	bosfsBin = "bosfs"
)

//go:generate mockgen -destination=./mock/mock_mounter.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/bos Mounter

type Mounter interface {
	common.FileSystem

	MountByBOSFS(ctx context.Context, source string, target string, options []string, sensitiveOptions []string) error
	UnmountFromBOSFS(ctx context.Context, target string) error
}

type bosfsMounter struct {
	common.FileSystem
	mount.Interface

	containerClient client.ContainerAPIClient
	imageClient     client.ImageAPIClient

	bosfsImage string
}

func newBOSFSMounter(bosfsImage string, containerClient client.ContainerAPIClient, imageClient client.ImageAPIClient) Mounter {
	return &bosfsMounter{
		FileSystem:      common.NewFS(),
		Interface:       mount.New(""),
		containerClient: containerClient,
		imageClient:     imageClient,
		bosfsImage:      bosfsImage,
	}
}

func (m *bosfsMounter) MountByBOSFS(ctx context.Context, source string, target string, options []string, sensitiveOptions []string) error {
	containerName := getContainerName(target)

	// Create bosfs container if it is not found.
	container, err := m.containerClient.ContainerInspect(ctx, containerName)
	if err != nil {
		if !client.IsErrNotFound(err) {
			glog.Errorf("[%s] Failed to get container by name: %s, err: %v", ctx.Value(util.TraceIDKey), containerName, err)
			return fmt.Errorf("failed to get bosfs container by name, err: %v", err)
		}

		containerID, err := m.createBosfsContainer(ctx, target, generateBosfsCMD(source, target, options, sensitiveOptions))
		if err != nil {
			glog.Errorf("[%s] Failed to create bosfs container, err: %v", ctx.Value(util.TraceIDKey), err)
			return fmt.Errorf("failed to create bosfs container, err: %v", err)
		}

		container, err = m.containerClient.ContainerInspect(ctx, containerID)
		if err != nil {
			glog.Errorf("[%s] Failed to get container by id after creation, id: %s, err: %v", ctx.Value(util.TraceIDKey), containerID, err)
			return fmt.Errorf("failed to get bosfs container by id, err: %v", err)
		}
	}

	// Can be one of "created", "running", "paused", "restarting", "removing", "exited", or "dead".
	switch container.State.Status {
	case "exited", "dead":
		// Remove existed/dead container. Do not recreate it.
		glog.Warningf("[%s] Bosfs container is existed or dead, id: %s, status: %s. Container should be removed.", ctx.Value(util.TraceIDKey), container.ID, container.State.Status)
		if err := m.containerClient.ContainerRemove(ctx, container.ID, types.ContainerRemoveOptions{Force: true}); err != nil {
			glog.Errorf("[%s] Failed to remove existed or dead bosfs container, id: %s, err: %s", ctx.Value(util.TraceIDKey), container.ID, err)
			return fmt.Errorf("failed to remove existed or dead container, err: %v", err)
		}
		return fmt.Errorf("bosfs container is existed or dead, retry later")
	case "running":
		// Status is one of starting, healthy or unhealthy
		if container.State.Health.Status == "healthy" {
			glog.V(4).Infof("[%s] Bosfs container is running and healthy, skip", ctx.Value(util.TraceIDKey))
			return nil
		}

	case "restarting", "removing":
		// Transient state. Let CO to retry.
		glog.Warningf("[%s] Bosfs container is %s, id: %s, retry later", ctx.Value(util.TraceIDKey), container.State.Status, container.ID)
		return fmt.Errorf("bosfs container is %s, retry later", container.State.Status)
	case "created", "paused":
		glog.V(4).Infof("[%s] Bosfs container status: %s, id: %s", ctx.Value(util.TraceIDKey), container.State.Status, container.ID)

		if err := m.containerClient.ContainerStart(ctx, container.ID, types.ContainerStartOptions{}); err != nil {
			glog.Errorf("[%s] Failed to start bosfs container, id: %s, err: %v", ctx.Value(util.TraceIDKey), container.ID, err)
			return fmt.Errorf("failed to start bosfs container, err: %v", err)
		}
	default:
		glog.Warningf("[%s] Unknown bosfs container: %s status: %s", ctx.Value(util.TraceIDKey), container.ID, container.State.Status)
	}

	glog.V(4).Infof("[%s] Bosfs container: %s starts", ctx.Value(util.TraceIDKey), container.ID)

	// Wait for container to be ready.
	subctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.waitForBosfsContainerReady(subctx, container.ID, 500*time.Millisecond); err != nil {
		glog.Errorf("[%s] Failed to wait for bosfs container: %s to be ready, err: %v", ctx.Value(util.TraceIDKey), container.ID, err)
		return fmt.Errorf("failed to wait for bosfs container to be ready, err: %v", err)
	}
	glog.V(4).Infof("[%s] Bosfs container is ready", ctx.Value(util.TraceIDKey))

	return nil
}

func (m *bosfsMounter) UnmountFromBOSFS(ctx context.Context, target string) error {
	containerName := getContainerName(target)

	container, err := m.containerClient.ContainerInspect(ctx, containerName)
	if err != nil && !client.IsErrNotFound(err) {
		glog.Errorf("[%s] Failed to get bosfs container by name: %s, err: %v", ctx.Value(util.TraceIDKey), containerName, err)
		return fmt.Errorf("failed to get bosfs container by name, err: %v", err)
	}

	// Stop and remove container if exists.
	if err == nil {
		if container.State.Status == "running" {
			if err := m.containerClient.ContainerStop(ctx, container.ID, nil); err != nil {
				glog.Errorf("[%s] Failed to stop bosfs container, id: %s, err: %v", ctx.Value(util.TraceIDKey), container.ID, err)
				return fmt.Errorf("failed to stop bosfs contianer, err: %v", err)
			}
		}

		if err := m.containerClient.ContainerRemove(ctx, container.ID, types.ContainerRemoveOptions{}); err != nil {
			glog.Errorf("[%s] Failed to remove bosfs container, id: %s, err: %v", ctx.Value(util.TraceIDKey), container.ID, err)
			return fmt.Errorf("failed to remove bosfs container, err: %v", err)
		}
	}

	glog.V(4).Infof("[%s] Bosfs container: %s stops", ctx.Value(util.TraceIDKey), containerName)

	// Unmount target path if it is a mount point.
	_, ref, err := mount.GetDeviceNameFromMount(m, target)
	if err != nil {
		glog.Errorf("[%s] Failed to check target path: %s mount status", ctx.Value(util.TraceIDKey), target)
		return fmt.Errorf("failed to check target path mount status, err: %v", err)
	}

	if ref > 0 {
		if err := m.Unmount(target); err != nil {
			glog.Errorf("[%s] Failed to unmount target path: %s, err: %v", ctx.Value(util.TraceIDKey), target, err)
			return err
		}
	}

	return nil
}

func (m *bosfsMounter) createBosfsContainer(ctx context.Context, targetPath, bosfsCMD string) (string, error) {
	containerName := getContainerName(targetPath)

	reader, err := m.imageClient.ImagePull(ctx, m.bosfsImage, types.ImagePullOptions{})
	if err != nil {
		glog.Errorf("[%s] Failed to pull bosfs image: %s, err: %v", ctx.Value(util.TraceIDKey), m.bosfsImage, err)
		return "", fmt.Errorf("failed to pull bosfs image, err: %v", err)
	}
	defer reader.Close()

	// TODO: Should we add timeout to ctx.
	if err := m.waitForImagePullFinish(ctx, m.bosfsImage, reader); err != nil {
		glog.Errorf("[%s] Failed to wait for bosfs image pulling finish, err: %v", ctx.Value(util.TraceIDKey), err)
		return "", fmt.Errorf("failed to wait for bosfs image pulling finish, err: %v", err)
	}
	glog.V(4).Infof("[%s] Bosfs image is pulled", ctx.Value(util.TraceIDKey))

	cmd := fmt.Sprintf("(umount %s || echo ignore error > /dev/null) && %s", targetPath, bosfsCMD)
	result, err := m.containerClient.ContainerCreate(
		ctx,
		&containertypes.Config{
			Entrypoint: []string{
				"/bin/bash",
			},
			Cmd: []string{
				"-c",
				cmd,
			},
			Image: m.bosfsImage,
			Healthcheck: &containertypes.HealthConfig{
				Test: []string{
					"CMD-SHELL",
					fmt.Sprintf("stat %s || exit 1", targetPath),
				},
				Interval:    5 * time.Second,
				Timeout:     3 * time.Second,
				StartPeriod: 400 * time.Millisecond,
				Retries:     3,
			},
		},
		&containertypes.HostConfig{
			Mounts: []mounttypes.Mount{
				{
					Type:   mounttypes.TypeBind,
					Source: path.Dir(targetPath),
					Target: path.Dir(targetPath),
					BindOptions: &mounttypes.BindOptions{
						Propagation: mounttypes.PropagationRShared,
					},
				},
				{
					Type:   mounttypes.TypeBind,
					Source: "/dev/fuse",
					Target: "/dev/fuse",
				},
				{
					Type:   mounttypes.TypeBind,
					Source: path.Dir(getCredentialsFilePath(targetPath)),
					Target: path.Dir(getCredentialsFilePath(targetPath)),
				},
			},
			RestartPolicy: containertypes.RestartPolicy{
				Name: "unless-stopped",
			},
			LogConfig: containertypes.LogConfig{
				Type: "json-file",
				Config: map[string]string{
					"max-file": "5",
					"max-size": "40m",
				},
			},
			NetworkMode: "host",
			Privileged:  true,
			CapAdd: []string{
				"SYS_ADMIN",
			},
		},
		nil,
		nil,
		containerName,
	)
	if err != nil {
		glog.Errorf("[%s] Failed to create bosfs container, err: %v", ctx.Value(util.TraceIDKey), err)
		return "", err
	}
	if len(result.Warnings) > 0 {
		glog.Warningf("[%s] Creating bosfs container warnings: %s", ctx.Value(util.TraceIDKey), util.ToJSONOrPanic(result.Warnings))
	}

	return result.ID, nil
}

func (m *bosfsMounter) waitForImagePullFinish(ctx context.Context, imageName string, eventReader io.Reader) error {
	d := json.NewDecoder(eventReader)
	var msg *jsonmessage.JSONMessage
	// Read until last message.
LOOP:
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waitting for image pulling")
		default:
			if err := d.Decode(&msg); err != nil {
				if err == io.EOF {
					break LOOP
				}
				glog.Errorf("[%s] Failed to read bosfs image pulling message, err: %v", ctx.Value(util.TraceIDKey), err)
				return err
			}
		}
	}

	if msg != nil {
		if msg.Error != nil && msg.Error.Message != "" {
			glog.Errorf("[%s] Failed to pull bosfs image, err: %v", ctx.Value(util.TraceIDKey), msg.Error.Message)
			return fmt.Errorf(msg.Error.Message)
		}
		if msg.ErrorMessage != "" {
			glog.Errorf("[%s] Failed to pull bosfs image, err: %v", ctx.Value(util.TraceIDKey), msg.ErrorMessage)
			return fmt.Errorf(msg.ErrorMessage)
		}
	}
	return nil
}

func (m *bosfsMounter) waitForBosfsContainerReady(ctx context.Context, containerID string, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for bosfs container ready")
		case <-ticker.C:
			container, err := m.containerClient.ContainerInspect(ctx, containerID)
			if err != nil {
				glog.Errorf("[%s] Failed to get bosfs container by id: %s, err: %v", ctx.Value(util.TraceIDKey), containerID, err)
				return fmt.Errorf("failed to get bosfs container when waiting for ready, err: %v", err)
			}
			if container.State.Status == "running" && container.State.Health.Status == "healthy" {
				return nil
			}
			glog.V(4).Infof("[%s] Waiting for bosfs container: %s to be ready, status: %s, health status: %s", ctx.Value(util.TraceIDKey), containerID, container.State.Status, container.State.Health.Status)
		}
	}
}

func generateBosfsCMD(source string, target string, options []string, sensitiveOptions []string) string {
	cmd := []string{
		bosfsBin,
		source,
		target,
		"-f",
	}
	cmd = append(cmd, options...)
	cmd = append(cmd, sensitiveOptions...)
	return strings.Join(cmd, " ")
}

func getContainerName(key string) string {
	hash := util.MD5(key)
	return "bosfs-" + hash
}

func getCredentialsFilePath(target string) string {
	return path.Join(credentialsFileDir, util.MD5(target), "sts")
}

func isErrTransportEndpointIsNotConnected(err error) bool {
	return strings.Contains(err.Error(), "transport endpoint is not connected")
}
