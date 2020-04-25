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
	"net"

	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"
	"go.ligato.io/vpp-agent/v3/proto/ligato/vpp"
	vpp_acl "go.ligato.io/vpp-agent/v3/proto/ligato/vpp/acl"
)

const (
	defaultIPv4NetworkString = "0.0.0.0/0"
	defaultIPv6NetworkString = "::/0"
)

func initVxlanACL(srcIP net.IP, conf *configurator.Config) error {
	iface, err := interfaceFromSrcIP(srcIP)
	if err != nil {
		return err
	}
	vxlanACL := &vpp.ACL{
		Name: iface.Name,
		Interfaces: &vpp_acl.ACL_Interfaces{
			Ingress: []string{iface.Name},
		},
	}
	nets, err := ipNetsFromInterface(iface)
	if err != nil {
		return err
	}
	for _, ipnet := range nets {
		for i := range ipnet.Mask {
			ipnet.Mask[i] = 0xff
		}
		_, srcNet, err := net.ParseCIDR(defaultIPv4NetworkString)
		if err != nil {
			return err
		}
		if ipnet.IP.To4() == nil {
			_, srcNet, err = net.ParseCIDR(defaultIPv6NetworkString)
			if err != nil {
				return err
			}
		}
		vxlanACL.Rules = append(vxlanACL.Rules, &vpp_acl.ACL_Rule{
			Action: vpp_acl.ACL_Rule_PERMIT,
			IpRule: &vpp_acl.ACL_Rule_IpRule{
				// Permit arp traffic
				Ip: &vpp_acl.ACL_Rule_IpRule_Ip{
					DestinationNetwork: ipnet.String(),
					SourceNetwork:      srcNet.String(),
				},
				Udp: &vpp_acl.ACL_Rule_IpRule_Udp{
					// Permit traffic to VXLAN Destination Ports
					DestinationPortRange: &vpp_acl.ACL_Rule_IpRule_PortRange{
						LowerPort: 4789,
						UpperPort: 4789,
					},
					// Permit traffic from all ports
					SourcePortRange: &vpp_acl.ACL_Rule_IpRule_PortRange{
						LowerPort: 0,
						UpperPort: 65535,
					},
				},
			},
		})
	}
	conf.GetVppConfig().Acls = append(conf.GetVppConfig().GetAcls(), vxlanACL)
	return nil
}
