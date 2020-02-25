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

package vppinit

import (
	"bufio"
	"io"
	"net"
	"os"
	"strings"

	"github.com/pkg/errors"
	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"
	"go.ligato.io/vpp-agent/v3/proto/ligato/vpp"
)

func arpEntries(iface *net.Interface) ([]*vpp.ARPEntry, error) {
	f, err := os.OpenFile("/proc/net/arp", os.O_RDONLY, 0600)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)

	var arps []*vpp.ARPEntry
	for l := 0; ; l++ {
		line, err := reader.ReadString('\n')

		if err != nil {
			if err != io.EOF {
				break
			}
			break
		}

		if l == 0 {
			continue // Skip first line with headers and empty line
		}
		if line == "" {
			break // Skip first line with headers and empty line
		}
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		ifaceName := strings.TrimSpace(parts[5])
		if ifaceName == iface.Name {
			arps = append(arps, &vpp.ARPEntry{
				PhysAddress: strings.TrimSpace(parts[3]),
				IpAddress:   strings.TrimSpace(parts[0]),
				Interface:   ifaceName,
			})
		}
	}
	return arps, nil
}

func initArpTable(srcIP net.IP, conf *configurator.Config) error {
	iface, err := interfaceFromSrcIP(srcIP)
	if err != nil {
		return err
	}
	entries, err := arpEntries(iface)
	if err != nil {
		return err
	}
	conf.GetVppConfig().Arps = append(conf.GetVppConfig().GetArps(), entries...)
	return nil
}
