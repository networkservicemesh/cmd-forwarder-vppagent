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
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/networkservicemesh/sdk/pkg/tools/flags"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffeutils"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/src/forwarder/internal/pkg/cmd"
)

func TestHealthCheck(t *testing.T) {
	logrus.Infof("*************************** TestHealthCheck**************************")
	f := pflag.NewFlagSet("testFlags", pflag.PanicOnError)
	cmd.Flags(f)
	flags.FromEnv(flags.EnvPrefix, flags.EnvReplacer, f)()

	tlsPeer, _ := spiffeutils.NewTLSPeer()
	clientCtx, clientCancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer clientCancelFunc()
	healthCC, err := grpc.DialContext(clientCtx, cmd.ListenOnURL.String(), grpc.WithBlock(), spiffeutils.WithSpiffe(tlsPeer, spiffeutils.DefaultTimeout))
	if err != nil {
		logrus.Fatalf("Failed healthcheck: %+v", err)
	}
	healthClient := grpc_health_v1.NewHealthClient(healthCC)
	healthResponse, err := healthClient.Check(clientCtx, &grpc_health_v1.HealthCheckRequest{
		Service: "networkservice.NetworkService",
	})
	assert.NoError(t, err)
	assert.NotNil(t, healthResponse)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, healthResponse.Status)
}
