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
	"os/signal"
	"syscall"

	"github.com/networkservicemesh/api/pkg/api/networkservice"
	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/chains/xconnectns"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

const (
	// NSMEnvVariablePrefix - all viper keys are transformed to all caps and prefixed with "%s_" % NSMEnvVariablePrefix
	NSMEnvVariablePrefix = "NSM"
	// EndpointNameKey - name of the endpoint for use in NSM
	EndpointNameKey = "endpoint_name"
	// TunnelIPKey - IP we can use for originating and terminating tunnels
	TunnelIPKey = "tunnel_ip"
	// VppagentURLKey - URL for reaching VPPAgent
	VppagentURLKey = "vppagent_url"
	// BasedirKey - key for the BaseDir to use for the memif mechanism
	BasedirKey = "base_dir"
	// ListenOnURLKey - Viper key for retrieving the URL to listen for incoming networkservicemesh RPC calls
	ListenOnURLKey = "listen_on_url"
	// ConnectToURKey - Viper key for retrieving the URL to make an outgoing networkservicemesh RPC calls
	ConnectToURKey = "connect_to_url"
)

func main() {
	// Capture signals to cleanup before exiting - note: this *must* be the first thing in main
	c := make(chan os.Signal, 1)
	signal.Notify(c,
		os.Interrupt,
		// More Linux signals here
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGQUIT)

	// Context to use for all things started in main
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	// Use Viper to capture information from the environment
	viper.SetEnvPrefix(NSMEnvVariablePrefix)
	_ = viper.BindEnv(TunnelIPKey)
	_ = viper.BindEnv(BasedirKey)
	_ = viper.BindEnv(ListenOnURLKey)
	viper.AutomaticEnv()

	// grpc.ClientConn for dialing the vppagent
	vppagentCC, _ := grpc.DialContext(ctx, viper.GetString(VppagentURLKey))

	// Tunnel IP for use for VXLAN and other Tunnel types
	tunnelIP := net.ParseIP(viper.GetString(TunnelIPKey))

	connectToURL, err := url.Parse(viper.GetString(ConnectToURKey))
	if err != nil {
		logrus.Fatalf("Please specify valid URL in %s Env Variable.  \"%s\" is not a valid URL", ConnectToURKey, viper.GetString(ConnectToURKey))
	}

	// XConnect Network Service Endpoint
	endpoint := xconnectns.NewServer(
		viper.GetString(EndpointNameKey),
		vppagentCC,
		viper.GetString(BasedirKey),
		tunnelIP,
		connectToURL, // url to reach up
	)

	// URL to listen on
	listenOnURL, err := url.Parse(viper.GetString(ListenOnURLKey))
	if err != nil {
		logrus.Fatalf("Please specify valid URL in %s Env Variable.  \"%s\" is not a valid URL", ListenOnURLKey, viper.Get(ListenOnURLKey))
	}

	// Create listener
	ln, err := net.Listen(listenOnURL.Scheme, listenOnURL.Host)
	if err != nil {
		logrus.Fatalf("failed to listen: %v", err)
	}

	// Create GRPC Server
	// TODO - add ServerOptions for Tracing, Security, etc
	server := grpc.NewServer()
	networkservice.RegisterNetworkServiceServer(server, endpoint)

	// Serve
	err = server.Serve(ln)
	if err != nil {
		logrus.Fatalf("failed to listen on %s", listenOnURL)
	}

	// Wait for signals
	<-c
}
