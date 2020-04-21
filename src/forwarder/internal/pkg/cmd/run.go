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

package cmd

import (
	"os"
	"time"

	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/networkservicemesh/api/pkg/api"

	"github.com/networkservicemesh/sdk-vppagent/pkg/tools/vppagent"
	"github.com/networkservicemesh/sdk/pkg/tools/errctx"
	"github.com/networkservicemesh/sdk/pkg/tools/grpcutils"
	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/chains/xconnectns"
	"github.com/networkservicemesh/sdk/pkg/tools/debug"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/src/forwarder/internal/pkg/authz"

	"github.com/networkservicemesh/sdk/pkg/tools/flags"
	"github.com/networkservicemesh/sdk/pkg/tools/spiffeutils"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/src/forwarder/internal/pkg/vppinit"
)

func init() {
	cmd := runCmd
	rootCmd.AddCommand(cmd)
	Flags(cmd.Flags())
	cobra.OnInitialize(flags.FromEnv(flags.EnvPrefix, flags.EnvReplacer, cmd.Flags()))
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Runs xconnect network service",
	Long: `Runs xconnect network service.  Supported mechanisms:
     - memif
     - kernel
     - vxlan`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := debug.Self(); err != nil {
			log.Entry(cmd.Context()).Infof("%s", err)
		}
		starttime := time.Now()

		// Run vppagent and get a connection to it
		vppagentCC, vppAgentCtx, err := vppagent.StartAndDialContext(cmd.Context())
		if err != nil {
			log.Entry(cmd.Context()).Fatalf("failed to dial vppagent with %+v", err)
		}

		// Get a tlsPeer to get credentials
		tlsPeer, err := spiffeutils.NewTLSPeer()
		if err != nil {
			log.Entry(cmd.Context()).Fatalf("Error attempting to create spiffeutils.TLSPeer %+v", err)
		}

		// Get OpenPolicyAgent Authz Policy
		authzPolicy, err := authz.PolicyFromFile(cmd.Context(), authz.AuthzRegoFilename, authz.DefaultAuthzRegoContents)
		if err != nil {
			log.Entry(cmd.Context()).Fatalf("Unable to open Authz policy file %q", authz.AuthzRegoFilename)
		}

		// XConnect Network Service Endpoint
		endpoint := xconnectns.NewServer(
			Name,
			&authzPolicy,
			spiffeutils.SpiffeJWTTokenGeneratorFunc(tlsPeer.GetCertificate, 10*time.Minute), // TODO get max token lifetime from a flag
			vppagentCC,
			BaseDir,
			TunnelIP,
			vppinit.Func(TunnelIP),
			&ConnectToURL,
			spiffeutils.WithSpiffe(tlsPeer, time.Second),
			grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		)

		// Create GRPC Server
		// TODO - add ServerOptions for Tracing
		server := grpc.NewServer(spiffeutils.SpiffeCreds(tlsPeer, time.Second))
		endpoint.Register(server)

		// Create GRPC Health Server:
		healthServer := health.NewServer()
		grpc_health_v1.RegisterHealthServer(server, healthServer)
		for _, service := range api.ServiceNames(endpoint) {
			healthServer.SetServingStatus(service, grpc_health_v1.HealthCheckResponse_SERVING)
		}

		srvCtx := grpcutils.ListenAndServe(cmd.Context(), &ListenOnURL, server)

		log.Entry(cmd.Context()).Infof("Startup completed in %v", time.Since(starttime))
		select {
		case <-cmd.Context().Done():
		case <-vppAgentCtx.Done():
			log.Entry(srvCtx).Warnf("vppagent exited: %+v", errctx.Err(vppAgentCtx))
		case <-srvCtx.Done():
			log.Entry(srvCtx).Warnf("failed to serve on %q: %+v", &ListenOnURL, errctx.Err(srvCtx))
			os.Exit(1)
		}
	},
}
