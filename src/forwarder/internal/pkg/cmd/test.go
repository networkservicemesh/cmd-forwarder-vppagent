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
	"os/exec"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/networkservicemesh/sdk/pkg/tools/debug"
	"github.com/networkservicemesh/sdk/pkg/tools/executils"
	"github.com/networkservicemesh/sdk/pkg/tools/flags"
	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/spire"
)

func init() {
	cmd := testCmd
	rootCmd.AddCommand(cmd)
	Flags(cmd.Flags())
	cobra.OnInitialize(flags.FromEnv(flags.EnvPrefix, flags.EnvReplacer, cmd.Flags()))
}

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Tests xconnect network service - DO NOT RUN IN PRODUCTION",
	Long:  `Tests xconnect network service - DO NOT RUN IN PRODUCTION`,
	Run: func(cmd *cobra.Command, args []string) {
		// Debug ourselves if env variable is set.  Make a non-default choice to not be confused with 'run'
		if err := debug.Self(debug.Dlv, "test", debug.Listen, path.Base(os.Args[0])); err != nil {
			log.Entry(cmd.Context()).Infof("%s", err)
		}

		// Run spire
		agentID := "spiffe://example.org/myagent"
		_, err := spire.Start(cmd.Context(), agentID)
		if err != nil {
			log.Entry(cmd.Context()).Fatalf("failed to run spire: %+v", err)
		}

		// Add spire entries
		if err = spire.AddEntry(cmd.Context(), agentID, "spiffe://example.org/forwarder", "unix:path:/bin/forwarder"); err != nil {
			log.Entry(cmd.Context()).Fatalf("failed to add entry to spire: %+v", err)
		}
		if err = spire.AddEntry(cmd.Context(), agentID, "spiffe://example.org/forwarder.test", "unix:path:/bin/forwarder.test"); err != nil {
			log.Entry(cmd.Context()).Fatalf("failed to add entry to spire: %+v", err)
		}

		// Run system under test (sut)
		cmdStr := os.Args[0] + " run " + strings.Join(args, "")
		ctx := log.WithField(cmd.Context(), "cmd", cmdStr)
		if _, err = executils.Start(ctx, cmdStr, executils.WithStdout(os.Stdout), executils.WithStderr(os.Stderr)); err != nil {
			log.Entry(cmd.Context()).Fatalf("Error running sut Executable: %q err: %q", cmdStr, err)
		}

		// Run the test
		testExecFilename, testErr := exec.LookPath(path.Base(os.Args[0]) + ".test")
		if testErr != nil {
			log.Entry(cmd.Context()).Fatalf("Unable to find test Executable: %q err: %q", path.Base(os.Args[0]), testErr)
		}
		// Run the test
		cmdStr = path.Base(os.Args[0]) + ".test"
		ctx = log.WithField(cmd.Context(), "cmd", cmdStr)
		if err := executils.Run(ctx, cmdStr); err != nil {
			log.Entry(cmd.Context()).Fatalf("Error running test Executable: %q err: %q", testExecFilename, err)
		}
	},
}
