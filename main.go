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

package main

import (
	"context"
	"net"
	"net/url"
	"os"
	"time"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/kelseyhightower/envconfig"

	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/chains/xconnectns"
	"github.com/networkservicemesh/sdk-vppagent/pkg/tools/vppagent"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/internal/authz"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/sdk/pkg/tools/debug"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/signalctx"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffeutils"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/internal/vppinit"
)

// Config - configuration for cmd-forwarder-vppagent
type Config struct {
	Name             string        `default:"forwarder" desc:"Name of Endpoint"`
	BaseDir          string        `default:"./" desc:"base directory" split_words:"true"`
	TunnelIP         net.IP        `desc:"IP to use for tunnels" split_words:"true"`
	ListenOn         url.URL       `default:"unix:///listen.on.socket" desc:"url to listen on" split_words:"true"`
	ConnectTo        url.URL       `default:"unix:///connect.to.socket" desc:"url to connect to" split_words:"true"`
	MaxTokenLifetime time.Duration `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`
}

func main() {
	// Setup context to catch signals
	ctx := signalctx.WithSignals(context.Background())
	ctx, cancel := context.WithCancel(ctx)

	// Setup logging
	logrus.SetFormatter(&nested.Formatter{})
	logrus.SetLevel(logrus.TraceLevel)
	ctx = log.WithField(ctx, "cmd", os.Args[0])

	// Debug self if necessary
	if err := debug.Self(); err != nil {
		log.Entry(ctx).Infof("%s", err)
	}

	starttime := time.Now()

	// Get config from environment
	config := &Config{}
	if err := envconfig.Usage("nsm", config); err != nil {
		logrus.Fatal(err)
	}
	if err := envconfig.Process("nsm", config); err != nil {
		logrus.Fatalf("error processing config from env: %+v", err)
	}

	log.Entry(ctx).Infof("Config: %#v", config)

	// Run vppagent and get a connection to it
	vppagentCC, vppagentErrCh := vppagent.StartAndDialContext(ctx)
	exitOnErr(ctx, cancel, vppagentErrCh)

	// Get a tlsPeer to get credentials
	tlsPeer, err := spiffeutils.NewTLSPeer()
	if err != nil {
		log.Entry(ctx).Fatalf("Error attempting to create spiffeutils.TLSPeer %+v", err)
	}

	// Get OpenPolicyAgent Authz Policy
	authzPolicy, err := authz.PolicyFromFile(ctx, authz.AuthzRegoFilename, authz.DefaultAuthzRegoContents)
	if err != nil {
		log.Entry(ctx).Fatalf("Unable to open Authz policy file %q", authz.AuthzRegoFilename)
	}

	// XConnect Network Service Endpoint
	endpoint := xconnectns.NewServer(
		config.Name,
		&authzPolicy,
		spiffeutils.SpiffeJWTTokenGeneratorFunc(tlsPeer.GetCertificate, config.MaxTokenLifetime),
		vppagentCC,
		config.BaseDir,
		config.TunnelIP,
		vppinit.Func(config.TunnelIP),
		&config.ConnectTo,
		spiffeutils.WithSpiffe(tlsPeer, time.Second),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)

	// Create GRPC Server
	// TODO - add ServerOptions for Tracing
	server := grpc.NewServer(spiffeutils.SpiffeCreds(tlsPeer, time.Second))
	endpoint.Register(server)
	srvErrCh := grpcutils.ListenAndServe(ctx, &config.ListenOn, server)
	exitOnErr(ctx, cancel, srvErrCh)
	log.Entry(ctx).Infof("Startup completed in %v", time.Since(starttime))

	<-ctx.Done()
	<-vppagentErrCh
}

func exitOnErr(ctx context.Context, cancel context.CancelFunc, errCh <-chan error) {
	// If we already have an error, log it and exit
	select {
	case err := <-errCh:
		log.Entry(ctx).Fatal(err)
	default:
	}
	// Otherwise wait for an error in the background to log and cancel
	go func(ctx context.Context, errCh <-chan error) {
		err := <-errCh
		log.Entry(ctx).Error(err)
		cancel()
	}(ctx, errCh)
}
