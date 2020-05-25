// Copyright (c) 2020 Cisco and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package vppinit provides a vppinit function for use with sdk-vppagent
package vppinit

import (
	"net"

	"github.com/pkg/errors"
	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"
)

// Func - returns the a function to create an initial vpp configuration
func Func(srcIP net.IP) func(conf *configurator.Config) error {
	var err error
	if srcIP == nil || srcIP.IsUnspecified() {
		srcIP, err = defaultTunnelIP()
	}
	return func(conf *configurator.Config) error {
		if err != nil {
			return errors.Wrap(err, "No tunnel IP provided")
		}
		if err := initInterface(srcIP, conf); err != nil {
			return err
		}
		if err := initArpTable(srcIP, conf); err != nil {
			return err
		}
		if err := initRoutes(conf); err != nil {
			return err
		}
		if err := initVxlanACL(srcIP, conf); err != nil {
			return err
		}
		return nil
	}
}
