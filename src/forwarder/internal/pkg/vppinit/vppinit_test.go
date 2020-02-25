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

package vppinit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/networkservicemesh/sdk-vppagent/pkg/networkservice/vppagent"
	"github.com/networkservicemesh/sdk/pkg/tools/log"

	"github.com/stretchr/testify/assert"
	"go.ligato.io/vpp-agent/v3/proto/ligato/configurator"

	"github.com/networkservicemesh/cmd-forwarder-vppagent/src/forwarder/internal/pkg/vppinit"

	vppagentexec "github.com/networkservicemesh/sdk-vppagent/pkg/tools/vppagent"
)

func TestVppInit(t *testing.T) {
	f := vppinit.Func(nil)
	ctx := vppagent.WithConfig(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	conf := vppagent.Config(ctx)
	err := f(conf)
	assert.NoError(t, err)
	// Run vppagent and get a connection to it
	vppagentCC, _, err := vppagentexec.StartAndDialContext(ctx)
	if err != nil {
		log.Entry(ctx).Fatalf("failed to dial vppagent with %+v", err)
	}
	client := configurator.NewConfiguratorServiceClient(vppagentCC)
	ctx, cancelFunc := context.WithTimeout(context.Background(), time.Second)
	defer cancelFunc()
	_, err = client.Update(ctx, &configurator.UpdateRequest{Update: conf, FullResync: true})
	assert.Nil(t, err)
	dump, err := client.Dump(ctx, &configurator.DumpRequest{})
	assert.Nil(t, err)

	j, _ := json.MarshalIndent(dump.Dump, "", "    ")
	fmt.Print(string(j))
	cancel()
}
