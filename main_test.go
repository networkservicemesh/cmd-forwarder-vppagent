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
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/exechelper"
	"github.com/edwarnicke/grpcfd"
	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/api/pkg/api/registry"
	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/registry/common/expire"
	registryrecvfd "github.com/networkservicemesh/sdk/pkg/registry/common/recvfd"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setid"
	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"
	"github.com/networkservicemesh/sdk/pkg/registry/memory"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/client"
	"github.com/networkservicemesh/sdk/pkg/networkservice/chains/endpoint"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/authorize"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms"
	kernelmechanism "github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/sendfd"
	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"
	"github.com/networkservicemesh/sdk/pkg/networkservice/ipam/point2pointipam"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffejwt"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/internal/ns"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/kelseyhightower/envconfig"

	"github.com/networkservicemesh/sdk/pkg/tools/spire"

	"github.com/vishvananda/netns"

	main "github.com/networkservicemesh/cmd-forwarder-vppagent"

	registrychain "github.com/networkservicemesh/sdk/pkg/registry/core/chain"
)

type ForwarderTestSuite struct {
	suite.Suite
	ctx          context.Context
	cancel       context.CancelFunc
	x509source   x509svid.Source
	x509bundle   x509bundle.Source
	config       main.Config
	spireErrCh   <-chan error
	sutErrCh     <-chan error
	rootNSHandle netns.NsHandle
	cc           grpc.ClientConnInterface
}

func (f *ForwarderTestSuite) SetupSuite() {
	logrus.SetFormatter(&nested.Formatter{})
	logrus.SetLevel(logrus.TraceLevel)
	f.ctx, f.cancel = context.WithCancel(context.Background())

	// Run spire
	executable, err := os.Executable()
	f.Require().NoError(err)
	f.spireErrCh = spire.Start(
		spire.WithContext(f.ctx),
		spire.WithEntry("spiffe://example.org/forwarder", "unix:path:/bin/forwarder"),
		spire.WithEntry(fmt.Sprintf("spiffe://example.org/%s", filepath.Base(executable)),
			fmt.Sprintf("unix:path:%s", executable),
		),
	)
	f.Require().Len(f.spireErrCh, 0)

	// Get X509Source
	source, err := workloadapi.NewX509Source(f.ctx)
	f.x509source = source
	f.x509bundle = source
	f.Require().NoError(err)
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
		exechelper.WithGracePeriod(30*time.Second),
	)
	f.Require().Len(f.sutErrCh, 0)

	// Get config from env
	f.Require().NoError(envconfig.Process("nsm", &f.config))

	// Grab the root NetNS handle
	f.rootNSHandle, err = netns.Get()
	f.Require().NoError(err)

	// Create a registryServer and registryClient
	memrg := memory.NewNetworkServiceEndpointRegistryServer()
	registryServer := registrychain.NewNetworkServiceEndpointRegistryServer(
		setid.NewNetworkServiceEndpointRegistryServer(),
		expire.NewNetworkServiceEndpointRegistryServer(),
		registryrecvfd.NewNetworkServiceEndpointRegistryServer(),
		memrg,
	)
	registryClient := adapters.NetworkServiceEndpointServerToClient(memrg)

	// Create a *grpc.Server
	serverCreds := credentials.NewTLS(tlsconfig.MTLSServerConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	serverCreds = grpcfd.TransportCredentials(serverCreds)
	server := grpc.NewServer(grpc.Creds(serverCreds))

	// Register the registryServer with the *grpc.Server
	registry.RegisterNetworkServiceEndpointRegistryServer(server, registryServer)
	ctx, cancel := context.WithCancel(f.ctx)
	defer func(cancel context.CancelFunc, serverErrCh <-chan error) {
		cancel()
		err = <-serverErrCh
		f.Require().NoError(err)
	}(cancel, f.ListenAndServe(ctx, server))

	// Get the regEndpoint
	recv, err := registryClient.Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			NetworkServiceNames: []string{f.config.NSName},
		},
		Watch: true,
	})
	f.Require().NoError(err)

	regEndpoint, err := recv.Recv()
	f.Require().NoError(err)
	log.Entry(ctx).Infof("Received regEndpoint: %+v", regEndpoint)

	// Dial the regEndpoint.GetUrl()
	clientCreds := credentials.NewTLS(tlsconfig.MTLSClientConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	clientCreds = grpcfd.TransportCredentials(clientCreds)
	f.cc, err = grpc.DialContext(f.ctx,
		regEndpoint.GetUrl(),
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithBlock(),
	)
	f.Require().NoError(err)
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
	f.Require().NotNil(healthResponse)
	f.Equal(grpc_health_v1.HealthCheckResponse_SERVING, healthResponse.Status)
}

func (f *ForwarderTestSuite) TestKernelToKernel() {
	// Create ctx for test
	ctx, cancel := context.WithTimeout(f.ctx, 10000*time.Second)
	defer cancel()

	networkserviceName := "testns"
	// Create testRequest
	testRequest := &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			NetworkService: networkserviceName,
		},
	}
	serverCreds := credentials.NewTLS(tlsconfig.MTLSServerConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	serverCreds = grpcfd.TransportCredentials(serverCreds)
	server := grpc.NewServer(grpc.Creds(serverCreds))

	serverNSName := "server"
	ep := f.server(ctx, serverNSName,
		map[string]networkservice.NetworkServiceServer{
			kernel.MECHANISM: chain.NewNetworkServiceServer(
				kernelmechanism.NewServer(),
			),
		},
	)
	networkservice.RegisterNetworkServiceServer(server, ep)
	networkservice.RegisterMonitorConnectionServer(server, ep)
	serverErrCh := f.ListenAndServe(ctx, server)

	clientNSName := "client"
	forwarderClient := f.client(ctx, clientNSName)

	// Send Request
	conn, err := forwarderClient.Request(ctx, testRequest)
	f.Require().NoError(err)
	f.NotNil(conn)

	// A word about the sleep here.  time.Sleep in tests is evil (in fact, its almost always evil :).
	// However, vppagent is *async* wrt applying our changes.  Meaning it takes our changes, returns, and then
	// gets around to applying them.  Normally its pretty zippy about it.  However we've gotten *so* fast that
	// its actually not always faster to apply them than we are to check them in this test.
	//
	// This sleep compensates for that.
	//
	// This sleep should *go away* shortly, when vppagent gets an option to fully apply the changes before
	// returning from the grpc call.  Till then, time.Sleep :(
	time.Sleep(200 * time.Millisecond)

	// Check the interfaces
	f.checkInterface(networkserviceName, conn.GetContext().GetIpContext().GetSrcIpAddr(), clientNSName)
	f.checkInterface(networkserviceName, conn.GetContext().GetIpContext().GetDstIpAddr(), serverNSName)

	// Check ping works both ways
	f.ping(conn.GetContext().GetIpContext().GetDstIpAddr(), clientNSName)
	f.ping(conn.GetContext().GetIpContext().GetSrcIpAddr(), serverNSName)

	_, err = forwarderClient.Close(ctx, conn)
	f.Require().NoError(err)

	// A word about the sleep here.  time.Sleep in tests is evil (in fact, its almost always evil :).
	// However, vppagent is *async* wrt applying our changes.  Meaning it takes our changes, returns, and then
	// gets around to applying them.  Normally its pretty zippy about it.  However we've gotten *so* fast that
	// its actually not always faster to apply them than we are to check them in this test.
	//
	// This sleep compensates for that.
	//
	// This sleep should *go away* shortly, when vppagent gets an option to fully apply the changes before
	// returning from the grpc call.  Till then, time.Sleep :(
	time.Sleep(200 * time.Millisecond)

	f.checkNoInterface(networkserviceName, clientNSName)
	f.checkNoInterface(networkserviceName, serverNSName)
	cancel()
	// TODO put a proper select with timeout here
	err = <-serverErrCh
	f.Require().NoError(err, "This line")
}

func (f *ForwarderTestSuite) client(ctx context.Context, nsName string) networkservice.NetworkServiceClient {
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
		f.cc,
		ns.NewClient(clientNSHandle),
		kernelmechanism.NewClient(),
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
		mechanisms.NewServer(mechanismMap),
		ns.NewServer(serverNSHandle),
		sendfd.NewServer(),
		ns.NewServer(f.rootNSHandle),
		point2pointipam.NewServer(prefix),
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

func (f *ForwarderTestSuite) ping(ipaddress, nsName string) {
	f.inNamedNS(nsName, func(nsName string) {
		ip, _, err := net.ParseCIDR(ipaddress)
		f.NoError(err)
		pingStr := fmt.Sprintf("ping -t 1 -c 1 %s", ip.String())
		f.NoError(
			exechelper.Run(pingStr,
				exechelper.WithEnvirons(os.Environ()...),
				exechelper.WithStdout(os.Stdout),
				exechelper.WithStderr(os.Stderr),
			),
		)
	})
}

func (f *ForwarderTestSuite) checkInterface(ifacePrefix, ipaddress, nsName string) {
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

func (f *ForwarderTestSuite) checkNoInterface(ifacePrefix, nsName string) {
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

func TestForwarderTestSuite(t *testing.T) {
	suite.Run(t, new(ForwarderTestSuite))
}
