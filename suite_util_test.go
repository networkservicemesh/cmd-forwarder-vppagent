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

// +build !windows

package main_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/edwarnicke/exechelper"
	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/ipam/point2pointipam"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/pkg/errors"
	"github.com/vishvananda/netns"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/vppagent"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/internal/ns"
)

const (
	contextTimeout = 20 * time.Second
)

func (f *ForwarderTestSuite) client(ctx context.Context, mechanismClient networkservice.NetworkServiceClient, nsName string) networkservice.NetworkServiceClient {
	clientNSHandle, err := newNamedNS(nsName)
	f.Require().NoError(err)
	go func(ctx context.Context, nsName string) {
		<-ctx.Done()
		f.Require().NoError(netns.DeleteNamed(nsName))
	}(ctx, nsName)
	// Create the kernelClient
	return client.NewClient(
		ctx,
		"testClient",
		nil,
		spiffejwt.TokenGeneratorFunc(f.x509source, f.config.MaxTokenLifetime),
		f.sutCC,
		ns.NewClient(clientNSHandle),
		mechanismClient,
		sendfd.NewClient(),
		ns.NewClient(f.rootNSHandle),
	)
}

func (f *ForwarderTestSuite) server(ctx context.Context, nsName string, mechanismMap map[string]networkservice.NetworkServiceServer) endpoint.Endpoint {
	serverNSHandle, err := newNamedNS(nsName)
	f.Require().NoError(err)

	// Launch test server
	_, prefix, err := net.ParseCIDR("10.0.0.0/24")
	f.Require().NoError(err)
	go func() {
		<-ctx.Done()
		f.Require().NoError(netns.DeleteNamed(nsName))
	}()
	return endpoint.NewServer(
		ctx,
		"testServer",
		authorize.NewServer(),
		spiffejwt.TokenGeneratorFunc(f.x509source, f.config.MaxTokenLifetime),
		vppagent.NewServer(),
		point2pointipam.NewServer(prefix),
		mechanisms.NewServer(mechanismMap),
		ns.NewServer(serverNSHandle),
		sendfd.NewServer(),
		ns.NewServer(f.rootNSHandle),
	)
}

func (f *ForwarderTestSuite) ListenAndServe(ctx context.Context, server *grpc.Server) <-chan error {
	errCh := grpcutils.ListenAndServe(ctx, &f.config.ConnectTo, server)
	select {
	case err, ok := <-errCh:
		f.Require().True(ok)
		f.Require().NoError(err)
	default:
	}
	returnErrCh := make(chan error, len(errCh)+1)
	go func(errCh <-chan error, returnErrCh chan<- error) {
		for err := range errCh {
			if err != nil {
				returnErrCh <- errors.Wrap(err, "ListenAndServe")
			}
		}
		close(returnErrCh)
	}(errCh, returnErrCh)
	return returnErrCh
}

func (f *ForwarderTestSuite) inNamedNS(nsName string, run func(nsName string)) {
	if nsName == "" {
		run(nsName)
		return
	}
	curNetns, err := netns.Get()
	f.Require().NoError(err)
	nsHandle, err := netns.GetFromName(nsName)
	f.Require().NoError(err)
	err = netns.Set(nsHandle)
	f.Require().NoError(err)
	run(nsName)
	err = netns.Set(curNetns)
	f.Require().NoError(err)
}

func (f *ForwarderTestSuite) pingKernel(ctx context.Context, ipaddress, nsName string) {
	f.inNamedNS(nsName, func(nsName string) {
		ip, _, err := net.ParseCIDR(ipaddress)
		f.NoError(err)
		pingStr := fmt.Sprintf("ping -c 1 %s", ip.String())
		for {
			select {
			case <-ctx.Done():
				f.FailNowf("", "unable to ping %s before context done: %+v", ipaddress, ctx.Err())
			default:
			}
			if err := exechelper.Run(pingStr,
				exechelper.WithEnvirons(os.Environ()...),
				exechelper.WithStdout(os.Stdout),
				exechelper.WithStderr(os.Stderr),
			); err != nil {
				continue
			}
			return
		}
	})
}

func (f *ForwarderTestSuite) pingVpp(ctx context.Context, ipaddress, rootDir string) {
	ip, _, err := net.ParseCIDR(ipaddress)
	f.NoError(err)
	pingStr := fmt.Sprintf("vppctl -s %s/var/run/vpp/cli.sock ping %s interval 0.1 repeat 1 verbose", rootDir, ip.String())
	for {
		select {
		case <-ctx.Done():
			f.FailNowf("", "unable to ping %s before context done: %+v", ipaddress, ctx.Err())
		default:
		}
		buf := bytes.NewBuffer([]byte{})
		f.NoError(
			exechelper.Run(pingStr,
				exechelper.WithEnvirons(os.Environ()...),
				exechelper.WithStdout(os.Stdout),
				exechelper.WithStderr(os.Stderr),
				exechelper.WithStdout(buf),
				exechelper.WithStderr(buf),
			),
		)
		if regexp.MustCompile(" 0% packet loss").Match(buf.Bytes()) {
			return
		}
	}
}

func (f *ForwarderTestSuite) checkKernelInterface(ifacePrefix, ipaddress, nsName string) {
	f.inNamedNS(nsName, func(nsName string) {
		links, err := net.Interfaces()
		f.NoErrorf(err, "Unable to find interface with prefix %q in netns %q", ifacePrefix, nsName)
		for _, link := range links {
			if !strings.HasPrefix(link.Name, ifacePrefix) {
				continue
			}
			addrs, err := link.Addrs()
			f.NoErrorf(err, "Unable to find interface with prefix %q in netns %q", ifacePrefix, nsName)
			for _, addr := range addrs {
				if addr.String() == ipaddress {
					return
				}
			}
			f.Fail("", "Interface %q in netns %q lacks ip address %q ", link.Name, nsName, ipaddress)
		}
		f.Failf("", "Unable to find interface with prefix %q in netns %q", ifacePrefix, nsName)
	})
}

func (f *ForwarderTestSuite) checkNoKernelInterface(ifacePrefix, nsName string) {
	f.inNamedNS(nsName, func(nsName string) {
		links, err := net.Interfaces()
		f.NoErrorf(err, "Unable to get interfaces in netns %q", nsName)
		for _, link := range links {
			if strings.HasPrefix(link.Name, ifacePrefix) {
				f.Fail("", "Interface %q in netns %q should not exist", link.Name, nsName)
			}
		}
	})
}

func newNamedNS(name string) (netns.NsHandle, error) {
	curNetns, err := netns.Get()
	if err != nil {
		return 0, err
	}
	defer func() { err = netns.Set(curNetns) }()

	// Create new netns
	newNetns, err := netns.NewNamed(name)
	if err != nil {
		return 0, err
	}
	return newNetns, nil
}
