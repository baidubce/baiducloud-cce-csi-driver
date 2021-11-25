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
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"
	mount "k8s.io/mount-utils"
	"k8s.io/utils/exec"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/common"
	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

const (
	bashCmd = "bash"
)

var (
	bashGrepDevSerialArgs = []string{
		"-c",
		"grep . /sys/block/vd*/serial",
	}
)

//go:generate mockgen -destination=./mock/mock_mounter.go -package=mock github.com/baidubce/baiducloud-cce-csi-driver/pkg/driver/cds Mounter

type Mounter interface {
	exec.Interface
	mount.Interface
	common.FileSystem
	GetDevPath(ctx context.Context, serial string) (string, error)
	GetDeviceSize(ctx context.Context, devPath string) (int64, error)
	ResizeFS(ctx context.Context, devPath, volumeID string) error
	GetDeviceNameFromMount(ctx context.Context, path string) (string, int, error)
	FormatAndMount(ctx context.Context, source string, target string, fstype string, options []string) error
}

type mounter struct {
	exec.Interface
	mount.SafeFormatAndMount
	common.FileSystem
	*mount.ResizeFs
}

func newMounter() Mounter {
	return &mounter{
		Interface: exec.New(),
		SafeFormatAndMount: mount.SafeFormatAndMount{
			Interface: mount.New(""),
			Exec:      exec.New(),
		},
		FileSystem: common.NewFS(),
		ResizeFs:   mount.NewResizeFs(exec.New()),
	}
}

// GetDevPath get device path by serial.
// GetDevPath use command `grep . /sys/block/vd*/serial` to get cds dev by serial.
// Example output:
//   /sys/block/vda/serial:v-s4Qub5lC
//   /sys/block/vdb/serial:v-3cBlhQtq
// It means:
//   v-s4Qub5lC => /dev/vda
//   v-3cBlhQtq => /dev/vdb
func (m *mounter) GetDevPath(ctx context.Context, serial string) (string, error) {
	if serial == "" {
		return "", fmt.Errorf("empty serial")
	}

	var stderr bytes.Buffer
	cmd := m.CommandContext(ctx, bashCmd, bashGrepDevSerialArgs...)
	cmd.SetStderr(&stderr)
	output, err := cmd.Output()
	if err != nil {
		glog.Errorf("[%s] Failed to exec `grep . /sys/block/vd*/serial`, stdout: %s, stderr: %s, err: %v",
			ctx.Value(util.TraceIDKey), string(output), stderr.String(), err)
		return "", err
	}

	glog.V(4).Infof("[%s] CMD `grep . /sys/block/vd*/serial` output: %s", ctx.Value(util.TraceIDKey), string(output))
	devices := strings.Split(string(output), "\n")
	for _, dev := range devices {
		if len(dev) == 0 {
			continue
		}
		kv := strings.Split(dev, ":")
		if len(kv) != 2 {
			glog.Warningf("[%s] Unexpected device serial line length %d: '%s'", ctx.Value(util.TraceIDKey), len(kv), dev)
			continue
		}
		if kv[1] == serial {
			// match serial
			strs := strings.Split(kv[0], "/")
			if len(strs) != 5 {
				glog.Warningf("[%s] Unexpected device name length %d: '%s'", ctx.Value(util.TraceIDKey), len(strs), kv[0])
				return "", fmt.Errorf("unexpected device serial line length %d: '%s'", len(strs), kv[0])
			}
			return "/dev/" + strs[3], nil
		}
	}
	return "", fmt.Errorf("serial %s not found", serial)
}

func (m *mounter) GetDeviceNameFromMount(ctx context.Context, path string) (string, int, error) {
	return mount.GetDeviceNameFromMount(m, path)
}

func (m *mounter) FormatAndMount(ctx context.Context, source string, target string, fstype string, options []string) error {
	glog.V(4).Infof("[%s] Mount %s to %s, fstype: %s, options: %+v", ctx.Value(util.TraceIDKey), source, target, fstype, options)
	return m.SafeFormatAndMount.FormatAndMount(source, target, fstype, options)
}

// GetDeviceSize is borrowed from "k8s.io/mount-utils".getDeviceSize
func (m *mounter) GetDeviceSize(ctx context.Context, devicePath string) (int64, error) {
	output, err := m.CommandContext(ctx, "blockdev", "--getsize64", devicePath).CombinedOutput()
	outStr := strings.TrimSpace(string(output))
	if err != nil {
		return 0, fmt.Errorf("failed to read size of device %s: %s: %s", devicePath, err, outStr)
	}
	size, err := strconv.ParseInt(outStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse size of device %s %s: %s", devicePath, outStr, err)
	}
	return size, nil
}

func (m *mounter) ResizeFS(ctx context.Context, devicePath, deviceMountPath string) error {
	resized, err := m.ResizeFs.Resize(devicePath, deviceMountPath)
	glog.V(4).Infof("[%s] resize %s mounted on %s: resized=%v, err=%v",
		ctx.Value(util.TraceIDKey), devicePath, deviceMountPath, resized, err)
	return err
}
