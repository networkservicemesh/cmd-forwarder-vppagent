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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/exechelper"
	"github.com/edwarnicke/grpcfd"
	"github.com/kelseyhightower/envconfig"
	"github.com/networkservicemesh/api/pkg/api/registry"
	vppagenttool "github.com/networkservicemesh/sdk-vppagent/pkg/tools/vppagent"
	"github.com/networkservicemesh/sdk/pkg/registry/common/expire"
	registryrecvfd "github.com/networkservicemesh/sdk/pkg/registry/common/recvfd"
	"github.com/networkservicemesh/sdk/pkg/registry/common/setid"
	"github.com/networkservicemesh/sdk/pkg/registry/core/adapters"
	registrychain "github.com/networkservicemesh/sdk/pkg/registry/core/chain"
	"github.com/networkservicemesh/sdk/pkg/registry/memory"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/spire"
	"github.com/phayes/freeport"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/vishvananda/netns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func (f *ForwarderTestSuite) SetupSuite() {
	logrus.SetFormatter(&nested.Formatter{})
	logrus.SetLevel(logrus.TraceLevel)
	f.ctx, f.cancel = context.WithCancel(context.Background())

	starttime := time.Now()

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Creating test vppagent Server (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	var err error
	f.vppagentServerRoot, err = ioutil.TempDir("", "vppagent-server-")
	f.Require().NoError(err)

	vppagentServerGrpcPort, err := freeport.GetFreePort()
	f.Require().NoError(err)

	vppagentServerHTTPPort, err := freeport.GetFreePort()
	f.Require().NoError(err)
	f.vppagentServerCC, f.vppagentServerErrCh = vppagenttool.StartAndDialContext(
		f.ctx,
		vppagenttool.WithRootDir(f.vppagentServerRoot),
		vppagenttool.WithGrpcPort(vppagentServerGrpcPort),
		vppagenttool.WithHTTPPort(vppagentServerHTTPPort),
	)
	f.Require().Len(f.vppagentServerErrCh, 0)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Creating test vppagent Client (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.vppagentClientRoot, err = ioutil.TempDir("", "vppagent-client-")
	f.Require().NoError(err)

	vppagentClientGrpcPort, err := freeport.GetFreePort()
	f.Require().NoError(err)

	vppagentClientHTTPPort, err := freeport.GetFreePort()
	f.Require().NoError(err)
	f.vppagentClientCC, f.vppagentClientErrCh = vppagenttool.StartAndDialContext(
		f.ctx,
		vppagenttool.WithRootDir(f.vppagentClientRoot),
		vppagenttool.WithGrpcPort(vppagentClientGrpcPort),
		vppagenttool.WithHTTPPort(vppagentClientHTTPPort),
	)
	f.Require().Len(f.vppagentClientErrCh, 0)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Running Spire (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
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

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Getting X509Source (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	source, err := workloadapi.NewX509Source(f.ctx)
	f.x509source = source
	f.x509bundle = source
	f.Require().NoError(err)
	svid, err := f.x509source.GetX509SVID()
	f.Require().NoError(err, "error getting x509 svid")
	log.Entry(f.ctx).Infof("SVID: %q received (time since start: %s)", svid.ID, time.Since(starttime))

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Running system under test (SUT) (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
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
	// ********************************************************************************
	log.Entry(f.ctx).Infof("Getting Config from Env (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.Require().NoError(envconfig.Process("nsm", &f.config))

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Grabbing the root netNS (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.rootNSHandle, err = netns.Get()
	f.Require().NoError(err)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Creating registryServer and registryClient (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	memrg := memory.NewNetworkServiceEndpointRegistryServer()
	registryServer := registrychain.NewNetworkServiceEndpointRegistryServer(
		setid.NewNetworkServiceEndpointRegistryServer(),
		expire.NewNetworkServiceEndpointRegistryServer(),
		registryrecvfd.NewNetworkServiceEndpointRegistryServer(),
		memrg,
	)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Get the regEndpoint from SUT (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	serverCreds := credentials.NewTLS(tlsconfig.MTLSServerConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	serverCreds = grpcfd.TransportCredentials(serverCreds)
	server := grpc.NewServer(grpc.Creds(serverCreds))

	registry.RegisterNetworkServiceEndpointRegistryServer(server, registryServer)
	ctx, cancel := context.WithCancel(f.ctx)
	defer func(cancel context.CancelFunc, serverErrCh <-chan error) {
		cancel()
		err = <-serverErrCh
		f.Require().NoError(err)
	}(cancel, f.ListenAndServe(ctx, server))

	recv, err := adapters.NetworkServiceEndpointServerToClient(memrg).Find(ctx, &registry.NetworkServiceEndpointQuery{
		NetworkServiceEndpoint: &registry.NetworkServiceEndpoint{
			NetworkServiceNames: []string{f.config.NSName},
		},
		Watch: true,
	})
	f.Require().NoError(err)

	regEndpoint, err := recv.Recv()
	f.Require().NoError(err)
	log.Entry(ctx).Infof("Received regEndpoint: %+v (time since start: %s)", regEndpoint, time.Since(starttime))

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Creating grpc.ClientConn to SUT (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	clientCreds := credentials.NewTLS(tlsconfig.MTLSClientConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	clientCreds = grpcfd.TransportCredentials(clientCreds)
	f.sutCC, err = grpc.DialContext(f.ctx,
		regEndpoint.GetUrl(),
		grpc.WithTransportCredentials(clientCreds),
		grpc.WithBlock(),
	)
	f.Require().NoError(err)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("SetupSuite Complete (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
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
	for {
		_, ok := <-f.vppagentServerErrCh
		if !ok {
			break
		}
	}
	for {
		_, ok := <-f.vppagentClientErrCh
		if !ok {
			break
		}
	}
}
