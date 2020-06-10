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

	"github.com/pkg/errors"
	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"
	vpp_interfaces "go.ligato.io/vpp-agent/v3/proto/ligato/vpp/interfaces"
)

func interfaceFromSrcIP(srcIP net.IP) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			switch v := addr.(type) {
			case *net.IPNet:
				if v.IP.Equal(srcIP) {
					return &iface, nil
				}
			default:
				continue
			}
		}
	}
	return nil, errors.Errorf("Unable to find interface with IP address: %s", srcIP.String())
}

func ipNetsFromInterface(iface *net.Interface) ([]*net.IPNet, error) {
	var rv []*net.IPNet
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			rv = append(rv, v)
		default:
			continue
		}
	}
	return rv, nil
}

func defaultTunnelIP() (net.IP, error) {
	excludedCIDRs := excludedCIDRs()
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, errors.WithStack(err)
		}
		for _, addr := range addrs {
			ip, _, _ := net.ParseCIDR(addr.String())
			usableAddr := true
			for _, excludedCIDR := range excludedCIDRs {
				if excludedCIDR.Contains(ip) {
					usableAddr = false
					break
				}
			}
			if !usableAddr {
				continue
			}
			return ip, nil
		}
	}
	return nil, errors.New("No usable tunnel ip found")
}

func excludedCIDRs() []*net.IPNet {
	excludedCIDRStrings := []string{
		"127.0.0.0/8",    // IPv4 Local Host
		"::1/128",        // IPv6 Local Host
		"169.254.0.0/16", // IPv4 Link Local
		"fe80::/10",      // IPv6 Link Local
	}
	var excludedCIDRs []*net.IPNet
	for _, excludedCIDRString := range excludedCIDRStrings {
		_, excludedCIDR, _ := net.ParseCIDR(excludedCIDRString)
		excludedCIDRs = append(excludedCIDRs, excludedCIDR)
	}
	return excludedCIDRs
}

func initInterface(srcIP net.IP, conf *configurator.Config) error {
	iface, err := interfaceFromSrcIP(srcIP)
	if err != nil {
		return err
	}
	nets, err := ipNetsFromInterface(iface)
	if err != nil {
		return err
	}
	vppIface := &vpp_interfaces.Interface{
		Name:        iface.Name,
		PhysAddress: iface.HardwareAddr.String(),
		Type:        vpp_interfaces.Interface_AF_PACKET,
		Enabled:     true,
		Link: &vpp_interfaces.Interface_Afpacket{
			Afpacket: &vpp_interfaces.AfpacketLink{
				HostIfName: iface.Name,
			},
		},
	}
	for _, ip := range nets {
		vppIface.IpAddresses = append(vppIface.IpAddresses, ip.String())
	}
	conf.GetVppConfig().Interfaces = append([]*vpp_interfaces.Interface{vppIface}, conf.GetVppConfig().GetInterfaces()...)
	return nil
}
