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

package cmd

import (
	"net"
	"net/url"

	"github.com/spf13/pflag"

	"github.com/networkservicemesh/sdk/pkg/tools/flags"
)

// Name of the endpoint
var Name string

// TunnelIP for the forwarder
var TunnelIP net.IP

// BaseDir - basedir for the forwarder
var BaseDir string

// ListenOnURL - where the forwarder should listen
var ListenOnURL url.URL

// ConnectToURL - the endpoint the forwarder should connect to
var ConnectToURL url.URL

// Flags - adds the flags for forwarder to f
func Flags(f *pflag.FlagSet) {
	// Name
	f.StringVarP(&Name, flags.NameKey, flags.NameShortHand, "forwarder",
		flags.NameUsageDefault)

	// Tunnel IP
	f.IPVarP(&TunnelIP, "tunnel-ip", "t", nil,
		"IP to use for originating and terminating tunnels")

	// BaseDir
	f.StringVarP(&BaseDir, flags.BaseDirKey, flags.BaseDirShortHand, flags.BaseDirDefault,
		flags.BaseDirUsageDefault)

	// Listen On URL
	flags.URLVarP(f, &ListenOnURL, flags.ListenOnURLKey, flags.ListenOnURLShortHand,
		&url.URL{Scheme: flags.ListenOnURLSchemeDefault, Path: flags.ListenOnURLPathDefault},
		flags.ListenOnURLUsageDefault)

	// Connect To URL
	flags.URLVarP(f, &ConnectToURL, flags.ConnectToURLKey, flags.ConnectToURLShortHand,
		&url.URL{Scheme: flags.ConnectToURLSchemeDefault, Path: flags.ConnectToURLPathDefault},
		flags.ConnectToURLUsageDefault)
}
