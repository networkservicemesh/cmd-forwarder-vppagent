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

package main_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/edwarnicke/exechelper"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/kelseyhightower/envconfig"

	"github.com/networkservicemesh/sdk/pkg/tools/spiffeutils"
	"github.com/networkservicemesh/sdk/pkg/tools/spire"

	main "github.com/networkservicemesh/cmd-forwarder-vppagent"
)

type ForwarderTestSuite struct {
	suite.Suite
	ctx        context.Context
	cancel     context.CancelFunc
	tlsPeer    spiffeutils.TLSPeer
	config     main.Config
	spireErrCh <-chan error
	sutErrCh   <-chan error
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

	// Get tslPeer
	f.tlsPeer, err = spiffeutils.NewTLSPeer()
	require.NoError(f.T(), err)

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
	ctx, cancel := context.WithTimeout(f.ctx, 100*time.Second)
	defer cancel()
	// TODO - this is where we fail.  Check to see if spire-agent is wired up correctly.
	healthCC, err := grpc.DialContext(ctx,
		f.config.ListenOn.String(),
		grpc.WithBlock(),
		spiffeutils.WithSpiffe(f.tlsPeer, spiffeutils.DefaultTimeout),
	)
	if err != nil {
		logrus.Fatalf("Failed healthcheck: %+v", err)
	}
	healthClient := grpc_health_v1.NewHealthClient(healthCC)
	healthResponse, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{
		Service: "networkservice.NetworkService",
	})
	f.NoError(err)
	f.NotNil(healthResponse)
	f.Equal(grpc_health_v1.HealthCheckResponse_SERVING, healthResponse.Status)
}

func TestForwarderTestSuite(t *testing.T) {
	suite.Run(t, new(ForwarderTestSuite))
}
