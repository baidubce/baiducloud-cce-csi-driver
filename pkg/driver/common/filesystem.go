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

package common

import (
	"context"
	"io/ioutil"
	"os"

	"k8s.io/utils/mount"
)

type FileSystem interface {
	PathExists(ctx context.Context, path string) (bool, error)
	MkdirAll(ctx context.Context, path string) error
	MakeFile(ctx context.Context, path string) error
	WriteFile(ctx context.Context, path string, content []byte) error
	RemovePath(ctx context.Context, path string) error
	RemoveAll(ctx context.Context, path string) error
}

type fs struct{}

func NewFS() FileSystem {
	return &fs{}
}

func (fs *fs) PathExists(ctx context.Context, path string) (bool, error) {
	return mount.PathExists(path)
}

func (fs *fs) MkdirAll(ctx context.Context, path string) error {
	err := os.MkdirAll(path, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (fs *fs) MakeFile(ctx context.Context, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE, os.FileMode(0644))
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return nil
}

func (fs *fs) WriteFile(ctx context.Context, path string, content []byte) error {
	return ioutil.WriteFile(path, content, os.FileMode(0644))
}

func (fs *fs) RemovePath(ctx context.Context, path string) error {
	return os.Remove(path)
}

func (fs *fs) RemoveAll(ctx context.Context, path string) error {
	return os.RemoveAll(path)
}
