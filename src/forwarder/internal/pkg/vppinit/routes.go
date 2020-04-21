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
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"
	"go.ligato.io/vpp-agent/v3/proto/ligato/vpp"
	vpp_l3 "go.ligato.io/vpp-agent/v3/proto/ligato/vpp/l3"
)

func defaultRoutes() ([]*vpp.Route, error) {
	var defaultRoutes []*vpp.Route
	// Note - we don't fail on error opening this file... because we could have only ipv6 routes
	f1, err := os.OpenFile("/proc/net/route", os.O_RDONLY, 0600)
	defer func() { _ = f1.Close() }()
	if err == nil {
		scanner := bufio.NewScanner(f1)
		ipv4defaultRoute, parseErr := parseProcNetRoute(scanner)
		if parseErr != nil {
			return nil, parseErr
		}
		if ipv4defaultRoute != nil {
			defaultRoutes = append(defaultRoutes, ipv4defaultRoute)
		}
	}

	f2, err := os.OpenFile(" /proc/net/ipv6_route", os.O_RDONLY, 0600)
	defer func() { _ = f2.Close() }()
	if err == nil {
		scanner := bufio.NewScanner(f2)
		ipv6defaultRoute, err := parseProcNetIPv6Route(scanner)
		if err != nil {
			return nil, err
		}
		if ipv6defaultRoute != nil {
			defaultRoutes = append(defaultRoutes, ipv6defaultRoute)
		}
	}
	// TODO - proper errors for failure to find *any* default route
	return defaultRoutes, nil
}

func parseProcNetRoute(scanner *bufio.Scanner) (*vpp.Route, error) {
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, errors.WithStack(err)
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			return nil, errors.New("Invalid /proc/net/route")
		}
		if strings.TrimSpace(parts[1]) == "00000000" {
			outgoingInterface := strings.TrimSpace(parts[0])
			defaultGateway := strings.TrimSpace(parts[2])
			ip, err := parseGatewayIP(defaultGateway)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			logrus.Printf("Found default gateway %v outgoing: %v", ip.String(), outgoingInterface)
			return &vpp.Route{
				Type:              vpp_l3.Route_INTER_VRF,
				OutgoingInterface: outgoingInterface,
				DstNetwork:        defaultIPv4NetworkString,
				Weight:            1,
				NextHopAddr:       ip.String(),
			}, nil
		}
	}
	return nil, errors.New("Invalid /proc/net/route")
}

func parseProcNetIPv6Route(scanner *bufio.Scanner) (*vpp.Route, error) {
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, errors.WithStack(err)
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 10 {
			return nil, errors.New("invalid /proc/net/ipv6_route")
		}
		if strings.TrimSpace(parts[0]) == "00000000000000000000000000000000" && strings.TrimSpace(parts[1]) == "00" {
			outgoingInterface := strings.TrimSpace(parts[9])
			defaultGateway := strings.TrimSpace(parts[4])
			ip, err := parseGatewayIP(defaultGateway)
			if err != nil {
				return nil, errors.WithStack(err)
			}
			logrus.Printf("Found default gateway %v outgoing: %v", ip.String(), outgoingInterface)
			return &vpp.Route{
				Type:              vpp_l3.Route_INTER_VRF,
				OutgoingInterface: outgoingInterface,
				DstNetwork:        defaultIPv6NetworkString,
				Weight:            1,
				NextHopAddr:       ip.String(),
			}, nil
		}
	}
	return nil, errors.New("Invalid /proc/net/route")
}

func parseGatewayIP(defaultGateway string) (net.IP, error) {
	if !(len(defaultGateway) == 8 || len(defaultGateway) == 32) {
		return nil, errors.New("failed to parse IP from string")
	}
	ip := net.IP(make([]byte, len(defaultGateway)/2))
	for i := 0; i < len(defaultGateway)/2; i++ {
		iv, err := strconv.ParseInt(defaultGateway[i*2:i*2+2], 16, 32)
		if err != nil {
			return nil, errors.Wrapf(err, "string does not represent a valid IP address")
		}
		ip[(len(defaultGateway)/2-1)-i] = byte(iv)
	}
	return ip, nil
}

func initRoutes(conf *configurator.Config) error {
	routes, err := defaultRoutes()
	if err != nil {
		return err
	}
	conf.GetVppConfig().Routes = append(conf.GetVppConfig().GetRoutes(), routes...)
	return nil
}
