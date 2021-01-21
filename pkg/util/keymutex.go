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

import "sync"

type LockedKey interface {
	String() string
}

type KeyMutex struct {
	mutex sync.Mutex
	kv    map[string]struct{}
}

// NewKeyMutex create a new KeyMutex.
func NewKeyMutex() *KeyMutex {
	return &KeyMutex{
		mutex: sync.Mutex{},
		kv:    make(map[string]struct{}),
	}
}

// TryLock try to lock at key, return whether locking is succeed.
func (m *KeyMutex) TryLock(key LockedKey) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, found := m.kv[key.String()]; found {
		return false
	}

	m.kv[key.String()] = struct{}{}
	return true
}

// Unlock unlock at key.
func (m *KeyMutex) Unlock(key LockedKey) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.kv, key.String())
}

type StringKey string

func (k StringKey) String() string {
	return string(k)
}
