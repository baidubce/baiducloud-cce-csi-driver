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
	"flag"

	"github.com/baidubce/bce-sdk-go/util/log"
	"github.com/golang/glog"
)

var (
	logToStderrFlagValue bool
	vFlagValue           glog.Level
)

func InitLog() {
	parseLogFlags()

	if logToStderrFlagValue {
		log.SetLogHandler(log.STDERR)
	}

	logLevel := int(log.PANIC) - int(vFlagValue)
	if logLevel < 0 {
		logLevel = 0
	}
	log.SetLogLevel(log.Level(logLevel))
}

func parseLogFlags() {
	if f := flag.Lookup("logtostderr"); f != nil {
		if getter, ok := f.Value.(flag.Getter); ok {
			if v, ok := getter.Get().(bool); ok {
				logToStderrFlagValue = v
			}
		}
	}

	if f := flag.Lookup("v"); f != nil {
		if getter, ok := f.Value.(flag.Getter); ok {
			if v, ok := getter.Get().(glog.Level); ok {
				vFlagValue = v
			}
		}
	}
}
