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
	"testing"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/stretchr/testify/suite"
	"github.com/vishvananda/netns"
	"google.golang.org/grpc"

	main "github.com/networkservicemesh/cmd-forwarder-vppagent"
)

type ForwarderTestSuite struct {
	suite.Suite
	ctx                 context.Context
	cancel              context.CancelFunc
	x509source          x509svid.Source
	x509bundle          x509bundle.Source
	config              main.Config
	spireErrCh          <-chan error
	sutErrCh            <-chan error
	rootNSHandle        netns.NsHandle
	sutCC               grpc.ClientConnInterface
	vppagentServerCC    grpc.ClientConnInterface
	vppagentServerRoot  string
	vppagentServerErrCh <-chan error
	vppagentClientCC    grpc.ClientConnInterface
	vppagentClientRoot  string
	vppagentClientErrCh <-chan error
}

func TestForwarderTestSuite(t *testing.T) {
	suite.Run(t, new(ForwarderTestSuite))
}
