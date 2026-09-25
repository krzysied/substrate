// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package overcommit

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/agent-substrate/substrate/internal/proto/ateletpb"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

func TestToAteletResources(t *testing.T) {
	if got := toAteletResources(nil); got != nil {
		t.Errorf("toAteletResources(nil) = %v, want nil", got)
	}
	in := &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: "250m"}, {Name: "memory", Quantity: "1Gi"}}}
	want := &ateletpb.Resources{Limits: []*ateletpb.Limits{{Name: "cpu", Quantity: "250m"}, {Name: "memory", Quantity: "1Gi"}}}
	if diff := cmp.Diff(want, toAteletResources(in), protocmp.Transform()); diff != "" {
		t.Errorf("toAteletResources() mismatch (-want +got):\n%s", diff)
	}
}
