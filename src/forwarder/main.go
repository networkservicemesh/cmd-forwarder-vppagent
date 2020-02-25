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
	"os"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/sirupsen/logrus"

	"github.com/networkservicemesh/sdk/pkg/tools/log"
	"github.com/networkservicemesh/sdk/pkg/tools/signalctx"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/src/forwarder/internal/pkg/cmd"
)

func main() {
	// Setup context to catch signals
	ctx := signalctx.WithSignals(context.Background())

	// Setup logging
	logrus.SetFormatter(&nested.Formatter{})
	logrus.SetLevel(logrus.TraceLevel)
	ctx = log.WithField(ctx, "cmd", os.Args[:2])

	// Execute command
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		logrus.Fatalf("error executing rootCmd: %v", err)
	}
}
