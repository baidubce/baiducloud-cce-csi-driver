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

func TestKeyMutex_TryLock(t *testing.T) {
	testCases := []struct {
		name            string
		key             LockedKey
		getMutex        func() *KeyMutex
		expectedSucceed bool
	}{
		{
			name: "succeed",
			key:  StringKey("test"),
			getMutex: func() *KeyMutex {
				return NewKeyMutex()
			},
			expectedSucceed: true,
		},
		{
			name: "failed",
			key:  StringKey("test"),
			getMutex: func() *KeyMutex {
				m := NewKeyMutex()
				m.TryLock(StringKey("test"))
				return m
			},
			expectedSucceed: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.getMutex()
			succeed := m.TryLock(tc.key)
			if succeed != tc.expectedSucceed {
				t.Errorf("expected succeed: %v, actual: %v", tc.expectedSucceed, succeed)
			}
		})
	}
}

func TestKeyMutex_Unlock(t *testing.T) {
	testCases := []struct {
		name     string
		key      LockedKey
		getMutex func() *KeyMutex
	}{
		{
			name: "succeed",
			key:  StringKey("test"),
			getMutex: func() *KeyMutex {
				m := NewKeyMutex()
				m.TryLock(StringKey("test"))
				return m
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.getMutex()
			m.Unlock(tc.key)
		})
	}
}
