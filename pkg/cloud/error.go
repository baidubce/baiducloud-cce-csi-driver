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
	"errors"
	"fmt"
	"net/http"

	"github.com/baidubce/bce-sdk-go/bce"
)

var (
	ErrMultiVolumes = fmt.Errorf("multi volumes with same name")
)

type invalidArgumentError struct {
	msg string
}

func ErrIsNotFound(err error) bool {
	var bceErr *bce.BceServiceError
	if errors.As(err, &bceErr) {
		return bceErr.StatusCode == http.StatusNotFound
	}

	return false
}

func ErrIsMultiVolumes(err error) bool {
	return errors.Is(err, ErrMultiVolumes)
}

func ErrIsInvalidArgument(err error) bool {
	var errInvalidArgument *invalidArgumentError
	return errors.As(err, &errInvalidArgument)
}

func newInvalidArgumentError(err error) error {
	return &invalidArgumentError{msg: err.Error()}
}

func (err *invalidArgumentError) Error() string {
	return err.msg
}
