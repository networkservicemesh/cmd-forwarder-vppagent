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

// +build linux

package main_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/exechelper"
	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/cls"
	"github.com/networkservicemesh/api/pkg/api/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	kernelmechanism "github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/ipam/point2pointipam"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/kelseyhightower/envconfig"

	"github.com/networkservicemesh/sdk/pkg/tools/spire"

	"github.com/vishvananda/netns"

	main "github.com/networkservicemesh/cmd-forwarder-vppagent"
)

type ForwarderTestSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	x509source x509svid.Source
	x509bundle x509bundle.Source
	config     main.Config
	spireErrCh <-chan error
	sutErrCh   <-chan error
	cc         grpc.ClientConnInterface
	client     networkservice.NetworkServiceClient
}

func (f *ForwarderTestSuite) SetupSuite() {
	logrus.SetFormatter(&nested.Formatter{})
	logrus.SetLevel(logrus.TraceLevel)
	f.ctx, f.cancel = context.WithCancel(context.Background())

	// Run spire
	executable, err := os.Executable()
	require.NoError(f.T(), err)
	f.spireErrCh = spire.Start(
		spire.WithContext(f.ctx),
		spire.WithEntry("spiffe://example.org/forwarder", "unix:path:/bin/forwarder"),
		spire.WithEntry(fmt.Sprintf("spiffe://example.org/%s", filepath.Base(executable)),
			fmt.Sprintf("unix:path:%s", executable),
		),
	)
	require.Len(f.T(), f.spireErrCh, 0)

	// Get X509Source
	source, err := workloadapi.NewX509Source(f.ctx)
	f.x509source = source
	f.x509bundle = source
	require.NoError(f.T(), err)
	svid, err := f.x509source.GetX509SVID()
	if err != nil {
		logrus.Fatalf("error getting x509 svid: %+v", err)
	}
	logrus.Infof("SVID: %q", svid.ID)

	// Run system under test (sut)
	cmdStr := "forwarder"
	f.sutErrCh = exechelper.Start(cmdStr,
		exechelper.WithContext(f.ctx),
		exechelper.WithEnvirons(os.Environ()...),
		exechelper.WithStdout(os.Stdout),
		exechelper.WithStderr(os.Stderr),
	)
	require.Len(f.T(), f.sutErrCh, 0)

	// Get config from env
	require.NoError(f.T(), envconfig.Process("nsm", &f.config))

	f.cc, err = grpc.DialContext(f.ctx,
		f.config.ListenOn.String(),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsconfig.MTLSClientConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))),
	)
	require.NoError(f.T(), err)

	f.client = client.NewClient(
		f.ctx,
		"testClient",
		nil,
		spiffejwt.TokenGeneratorFunc(f.x509source, f.config.MaxTokenLifetime),
		f.cc,
	)
}

func (f *ForwarderTestSuite) TearDownSuite() {
	f.cancel()
	for {
		_, ok := <-f.sutErrCh
		if !ok {
			break
		}
	}
	for {
		_, ok := <-f.spireErrCh
		if !ok {
			break
		}
	}
}

func (f *ForwarderTestSuite) TestHealthCheck() {
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	healthClient := grpc_health_v1.NewHealthClient(f.cc)
	healthResponse, err := healthClient.Check(ctx,
		&grpc_health_v1.HealthCheckRequest{
			Service: "networkservice.NetworkService",
		},
		grpc.WaitForReady(true),
	)
	f.NoError(err)
	require.NotNil(f.T(), healthResponse)
	f.Equal(grpc_health_v1.HealthCheckResponse_SERVING, healthResponse.Status)
}

func (f *ForwarderTestSuite) TestKernelToKernel() {
	// Create ctx for test
	ctx, cancel := context.WithTimeout(f.ctx, 100*time.Second)
	defer cancel()

	// Create client netns
	clientNetnsName := "client"

	// Create client netns
	clientNS, err := newNS()
	require.NoError(f.T(), err)
	_, err = bindNS(clientNS, clientNetnsName)
	require.NoError(f.T(), err)
	_, inode, err := getInode(clientNS)
	require.NoError(f.T(), err)
	f.Require().NoError(inNS(clientNS, func() {
		exechelper.Start("tail -f /dev/null", exechelper.WithContext(ctx))
	}))

	networkserviceName := "testns"
	// Create testRequest
	testRequest := &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			NetworkService: networkserviceName,
		},
		MechanismPreferences: []*networkservice.Mechanism{
			{
				Cls:  cls.LOCAL,
				Type: kernel.MECHANISM,
				Parameters: map[string]string{
					kernel.NetNSInodeKey: strconv.FormatUint(inode, 10),
				},
			},
		},
	}

	// Launch test server
	_, prefix, err := net.ParseCIDR("10.0.0.0/24")
	require.NoError(f.T(), err)
	ep := endpoint.NewServer(
		ctx,
		"testServer",
		authorize.NewServer(),
		spiffejwt.TokenGeneratorFunc(f.x509source, f.config.MaxTokenLifetime),
		mechanisms.NewServer(map[string]networkservice.NetworkServiceServer{
			kernel.MECHANISM: kernelmechanism.NewServer(),
		}),
		point2pointipam.NewServer(prefix),
	)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsconfig.MTLSServerConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))))
	ep.Register(server)

	grpcutils.ListenAndServe(f.ctx, &f.config.ConnectTo, server)

	// Send Request
	conn, err := f.client.Request(ctx, testRequest, grpc.WaitForReady(true))
	require.NoError(f.T(), err)
	f.NotNil(conn)

	// Check the interfaces
	// f.NoError(checkInterface(networkserviceName, conn.GetContext().GetIpContext().GetSrcIpAddr(), clientNetnsName))
	f.NoError(checkInterface(networkserviceName, conn.GetContext().GetIpContext().GetSrcIpAddr(), clientNetnsName))
	f.NoError(checkInterface(networkserviceName, conn.GetContext().GetIpContext().GetDstIpAddr(), ""))

	// Check ping works both ways
	f.NoError(ping(conn.GetContext().GetIpContext().GetDstIpAddr(), clientNetnsName))
	f.NoError(ping(conn.GetContext().GetIpContext().GetSrcIpAddr(), ""))
}

func bindNS(handle netns.NsHandle, name string) (string, error) {
	// Anchor netns file in /run/netns so that iproute2 can access it
	netnsdir := "/run/netns/"
	err := os.MkdirAll(netnsdir, 0750)
	if err != nil {
		return "", err
	}
	netnspath := filepath.Join(netnsdir, name)
	netnsfile, err := os.Create(netnspath)
	if err != nil {
		return "", err
	}
	err = netnsfile.Close()
	if err != nil {
		return "", err
	}
	procpath := fmt.Sprintf("/proc/self/fd/%d", uintptr(handle))
	err = unix.Mount(procpath, netnspath, "none", unix.MS_BIND, "")
	if err != nil {
		return "", err
	}
	return netnspath, nil
}

func getInode(handle netns.NsHandle) (dev, ino uint64, err error) {
	var stat syscall.Stat_t
	err = syscall.Fstat(int(handle), &stat)
	if err != nil {
		return 0, 0, err
	}
	return stat.Dev, stat.Ino, err
}

func newNS() (netns.NsHandle, error) {
	curNetns, err := netns.Get()
	if err != nil {
		return 0, err
	}

	// Create new netns
	newNetns, err := netns.New()
	if err != nil {
		return 0, err
	}
	err = netns.Set(curNetns)
	if err != nil {
		return 0, err
	}
	return newNetns, nil
}

func inNS(ns netns.NsHandle, f func()) error {
	curNetns, err := netns.Get()
	if err != nil {
		return err
	}
	err = netns.Set(ns)
	if err != nil {
		return err
	}
	f()
	err = netns.Set(curNetns)
	if err != nil {
		return err
	}
	return nil
}

func ping(ipaddress, netNSName string) error {
	ip, _, err := net.ParseCIDR(ipaddress)
	if err != nil {
		return err
	}
	var pingStr string
	if netNSName != "" {
		pingStr = fmt.Sprintf("ip netns exec %s ", netNSName)
	}
	pingStr = fmt.Sprintf("%s ping -t 1 -c 1 %s", pingStr, ip.String())
	return exechelper.Run(pingStr,
		exechelper.WithEnvirons(os.Environ()...),
		exechelper.WithStdout(os.Stdout),
		exechelper.WithStderr(os.Stderr))
}

func checkInterface(ifacePrefix, ipaddress, netNS string) error {
	ip, ipNet, err := net.ParseCIDR(ipaddress)
	if err != nil {
		return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
	}
	if netNS != "" {
		var curnetNS netns.NsHandle
		curnetNS, err = netns.Get()
		if err != nil {
			return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
		}
		defer func() { _ = netns.Set(curnetNS) }()
		var nshandle netns.NsHandle
		nshandle, err = netns.GetFromName(netNS)
		if err != nil {
			return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
		}
		err = netns.Set(nshandle)
		if err != nil {
			return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
		}
	}
	links, err := netlink.LinkList()
	if err != nil {
		return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
	}
	for _, link := range links {
		if !strings.HasPrefix(link.Attrs().Name, ifacePrefix) {
			continue
		}
		addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return errors.Wrapf(err, "Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
		}
		for _, addr := range addrs {
			if addr.IP.Equal(ip) && addr.Mask.String() == ipNet.Mask.String() {
				return nil
			}
		}
		return errors.Errorf("Interface %s lacks ip address %s", link.Attrs().Name, ipaddress)
	}
	return errors.Errorf("Unable to find interface with prefix %s in netns %s", ifacePrefix, netNS)
}

func TestForwarderTestSuite(t *testing.T) {
	suite.Run(t, new(ForwarderTestSuite))
}
