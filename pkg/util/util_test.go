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

package util

import "testing"

func TestRoundUpGB(t *testing.T) {
	testCases := []struct {
		name           string
		sizeBytes      int64
		expectedSizeGB int64
	}{
		{
			name:           "0 GB",
			sizeBytes:      0,
			expectedSizeGB: 0,
		},
		{
			name:           "5 GB (without ceil)",
			sizeBytes:      5 * GB,
			expectedSizeGB: 5,
		},
		{
			name:           "10 GB (ceil)",
			sizeBytes:      9*GB + 1,
			expectedSizeGB: 10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sizeGB := RoundUpGB(tc.sizeBytes)
			if sizeGB != tc.expectedSizeGB {
				t.Errorf("expected: %d, actual: %d", tc.expectedSizeGB, sizeGB)
			}
		})
	}
}
