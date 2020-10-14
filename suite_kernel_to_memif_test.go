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
	"io/ioutil"
	"os"
	"time"

	"github.com/edwarnicke/grpcfd"
	kernelmechanism "github.com/networkservicemesh/sdk/pkg/networkservice/common/mechanisms/kernel"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/commit"
	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/connectioncontext"
	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/mechanisms/memif"

	"github.com/networkservicemesh/sdk/pkg/networkservice/core/chain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func (f *ForwarderTestSuite) TestKernelToMemif() {
	starttime := time.Now()
	// Create ctx for test
	ctx, cancel := context.WithTimeout(f.ctx, contextTimeout)
	defer cancel()
	ctx = log.WithField(ctx, "TestKernelToMemif", f.T().Name())

	networkserviceName := "k2mns"
	// Create testRequest
	testRequest := &networkservice.NetworkServiceRequest{
		Connection: &networkservice.Connection{
			NetworkService: networkserviceName,
		},
	}

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Launching %s test server (time since start: %s)", f.T().Name(), time.Since(starttime))
	// ********************************************************************************
	serverCreds := credentials.NewTLS(tlsconfig.MTLSServerConfig(f.x509source, f.x509bundle, tlsconfig.AuthorizeAny()))
	serverCreds = grpcfd.TransportCredentials(serverCreds)
	server := grpc.NewServer(grpc.Creds(serverCreds))

	serverNSName := "k2m-server"
	tmpDir, err := ioutil.TempDir("", "forwarder.test-")
	if err != nil {
		logrus.Fatalf("error creating tmpDir %+v", err)
	}
	defer func(tmpDir string) { _ = os.Remove(tmpDir) }(tmpDir)
	ep := f.server(ctx, serverNSName,
		map[string]networkservice.NetworkServiceServer{
			memif.MECHANISM: chain.NewNetworkServiceServer(
				memif.NewServer(tmpDir),
				connectioncontext.NewServer(),
				commit.NewServer(f.vppagentServerCC),
			),
		},
	)
	networkservice.RegisterNetworkServiceServer(server, ep)
	networkservice.RegisterMonitorConnectionServer(server, ep)
	serverErrCh := f.ListenAndServe(ctx, server)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Sending Request to forwarder (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	clientNSName := "k2m-client"
	forwarderClient := f.client(ctx, kernelmechanism.NewClient(), clientNSName)

	// Send Request
	conn, err := forwarderClient.Request(ctx, testRequest)
	f.Require().NoError(err)
	f.NotNil(conn)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Checking we can ping both ways (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.pingKernel(ctx, conn.GetContext().GetIpContext().GetDstIpAddr(), clientNSName)
	f.pingVpp(ctx, conn.GetContext().GetIpContext().GetSrcIpAddr(), f.vppagentServerRoot)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Checking that the expected interfaces exist (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.checkKernelInterface(networkserviceName, conn.GetContext().GetIpContext().GetSrcIpAddr(), clientNSName)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Sending Close to forwarder (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
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
	// ********************************************************************************
	log.Entry(f.ctx).Infof("Sleeping 200ms (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	time.Sleep(200 * time.Millisecond)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Checking that the expected interfaces no longer exist (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	f.checkNoKernelInterface(networkserviceName, clientNSName)

	// ********************************************************************************
	log.Entry(f.ctx).Infof("Canceling ctx to end test (time since start: %s)", time.Since(starttime))
	// ********************************************************************************
	cancel()
	// TODO put a proper select with timeout here
	err = <-serverErrCh
	f.Require().NoError(err, "This line")
	// ********************************************************************************
	log.Entry(f.ctx).Infof("%s completed (time since start: %s)", f.T().Name(), time.Since(starttime))
	// ********************************************************************************
}
