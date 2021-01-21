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
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/utils/exec"
	exectesting "k8s.io/utils/exec/testing"
)

func TestMounter_GetDevPath(t *testing.T) {
	cmdOutput := `/sys/block/vda/serial:v-xxxx01
/sys/block/vdb/serial:v-xxxx02
/sys/block/vdc/serial:v-xxxx03`

	type mocks struct {
		mounter Mounter
	}

	testCases := []struct {
		name        string
		mocks       mocks
		serial      string
		expectedDev string
		expectedErr error
	}{
		{
			name: "get dev by serial succeed",
			mocks: func() mocks {
				fakeCmd := &exectesting.FakeCmd{}
				fakeCmd.OutputScript = append(fakeCmd.OutputScript, func() ([]byte, []byte, error) {
					return []byte(cmdOutput), nil, nil
				})

				fakeExec := &exectesting.FakeExec{}
				fakeExec.CommandScript = append(fakeExec.CommandScript, func(cmd string, args ...string) exec.Cmd {
					return fakeCmd
				})

				mounter := &mounter{
					Interface: fakeExec,
				}
				return mocks{mounter: mounter}
			}(),
			serial:      "v-xxxx02",
			expectedDev: "/dev/vdb",
			expectedErr: nil,
		},
		{
			name: "serial not found",
			mocks: func() mocks {
				fakeCmd := &exectesting.FakeCmd{}
				fakeCmd.OutputScript = append(fakeCmd.OutputScript, func() ([]byte, []byte, error) {
					return []byte(cmdOutput), nil, nil
				})

				fakeExec := &exectesting.FakeExec{}
				fakeExec.CommandScript = append(fakeExec.CommandScript, func(cmd string, args ...string) exec.Cmd {
					return fakeCmd
				})

				mounter := &mounter{
					Interface: fakeExec,
				}
				return mocks{mounter: mounter}
			}(),
			serial:      "v-xxxx04",
			expectedDev: "",
			expectedErr: fmt.Errorf("serial %s not found", "v-xxxx04"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dev, err := tc.mocks.mounter.GetDevPath(context.Background(), tc.serial)
			if !cmp.Equal(err, tc.expectedErr, cmp.Comparer(func(x, y error) bool {
				if x == nil || y == nil {
					return (x == nil) == (y == nil)
				}
				return x.Error() == y.Error()
			})) {
				t.Errorf("expected err: %v, actual: %v", tc.expectedErr, err)
			}
			if !cmp.Equal(dev, tc.expectedDev) {
				t.Errorf("expected dev: %s, actual: %s", tc.expectedDev, dev)
			}
		})
	}
}
