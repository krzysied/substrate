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

package workerservice

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/ateletauth/ateletauthtest"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store/storetest"
	"github.com/agent-substrate/substrate/internal/apierror"
	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// seedAllocatedWorker registers a Worker on testNode hosting one Actor that
// books 1 CPU and 1Gi.
func seedAllocatedWorker(t *testing.T, st store.Interface) {
	t.Helper()
	seedReportedWorker(t, st, testNode, &ateapipb.WorkerResources{Actors: 4, Resources: resources.CPUMemory(4000, 8<<30)})
	err := st.BindActorToWorker(context.Background(), testWorkerName, &ateapipb.ActorAssignment{
		Actor:     &ateapipb.ObjectRef{Atespace: "demo", Name: "actor-a"},
		ActorUid:  "uid-a",
		Resources: resources.CPUMemory(1000, 1<<30),
	}, nil)
	if err != nil {
		t.Fatalf("BindActorToWorker: %v", err)
	}
}

func workerAllocated(t *testing.T, st store.Interface) *ateapipb.WorkerResources {
	t.Helper()
	w, err := st.GetWorker(context.Background(), testWorkerName)
	if err != nil {
		t.Fatalf("GetWorker: %v", err)
	}
	return w.GetStatus().GetAllocated()
}

func TestAddWorkerAllocated(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st, &fakeSuspender{}, testAteletSPIFFEID, nil, "", nil)
	seedAllocatedWorker(t, st)

	_, err := s.AddWorkerAllocated(ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)), &ateapipb.AddWorkerAllocatedRequest{
		Worker:        &ateapipb.ObjectRef{Name: testWorkerName},
		ResourceDelta: resources.CPUMemory(500, 1<<30),
	})
	if err != nil {
		t.Fatalf("AddWorkerAllocated() failed: %v", err)
	}
	want := &ateapipb.WorkerResources{Actors: 1, Resources: resources.CPUMemory(1500, 2<<30)}
	if diff := cmp.Diff(want, workerAllocated(t, st), protocmp.Transform()); diff != "" {
		t.Errorf("allocated mismatch (-want +got):\n%s", diff)
	}
}

// Handing back more than is booked clamps at zero; the actor count is left
// alone.
func TestSubtractWorkerAllocated(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st, &fakeSuspender{}, testAteletSPIFFEID, nil, "", nil)
	seedAllocatedWorker(t, st)

	_, err := s.SubtractWorkerAllocated(ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode)), &ateapipb.SubtractWorkerAllocatedRequest{
		Worker:        &ateapipb.ObjectRef{Name: testWorkerName},
		ResourceDelta: resources.CPUMemory(2000, 512<<20),
	})
	if err != nil {
		t.Fatalf("SubtractWorkerAllocated() failed: %v", err)
	}
	want := &ateapipb.WorkerResources{Actors: 1, Resources: resources.CPUMemory(0, 512<<20)}
	if diff := cmp.Diff(want, workerAllocated(t, st), protocmp.Transform()); diff != "" {
		t.Errorf("allocated mismatch (-want +got):\n%s", diff)
	}
}

// Both RPCs share one path, so each error case is run against both.
func TestAdjustWorkerAllocated_Errors(t *testing.T) {
	st, cleanup := storetest.SetupTestStore(t)
	defer cleanup()
	s := New(st, &fakeSuspender{}, testAteletSPIFFEID, nil, "", nil)
	seedAllocatedWorker(t, st)
	authed := ateletauthtest.ContextWith(ateletauthtest.CertOn(t, testNode))
	worker := &ateapipb.ObjectRef{Name: testWorkerName}
	delta := resources.CPUMemory(100, 0)

	rpcs := map[string]func(context.Context, *ateapipb.ObjectRef, *ateapipb.Resources) error{
		"add": func(ctx context.Context, w *ateapipb.ObjectRef, d *ateapipb.Resources) error {
			_, err := s.AddWorkerAllocated(ctx, &ateapipb.AddWorkerAllocatedRequest{Worker: w, ResourceDelta: d})
			return err
		},
		"subtract": func(ctx context.Context, w *ateapipb.ObjectRef, d *ateapipb.Resources) error {
			_, err := s.SubtractWorkerAllocated(ctx, &ateapipb.SubtractWorkerAllocatedRequest{Worker: w, ResourceDelta: d})
			return err
		},
	}
	tests := []struct {
		name   string
		ctx    context.Context
		worker *ateapipb.ObjectRef
		delta  *ateapipb.Resources
		want   codes.Code
	}{
		{"unauthenticated", ateletauthtest.ContextWith(nil), worker, delta, codes.Unauthenticated},
		{"no worker ref", authed, nil, delta, codes.InvalidArgument},
		{"worker ref with atespace", authed, &ateapipb.ObjectRef{Atespace: "demo", Name: testWorkerName}, delta, codes.InvalidArgument},
		{"no delta", authed, worker, nil, codes.InvalidArgument},
		{"negative quantity", authed, worker, resources.CPUMemory(-100, 0), codes.InvalidArgument},
		{"unparseable quantity", authed, worker, &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: "lots"}}}, codes.InvalidArgument},
		{"absent worker", authed, &ateapipb.ObjectRef{Name: "3b9f1e77-2c4d-4a80-91be-6d5c8f0a7e21"}, delta, codes.NotFound},
		{"worker on another node", ateletauthtest.ContextWith(ateletauthtest.CertOn(t, "some-other-node")), worker, delta, codes.NotFound},
	}
	for rpc, call := range rpcs {
		for _, tc := range tests {
			t.Run(rpc+"/"+tc.name, func(t *testing.T) {
				if got := apierror.Code(call(tc.ctx, tc.worker, tc.delta)); got != tc.want {
					t.Errorf("code = %v, want %v", got, tc.want)
				}
			})
		}
	}

	// None of the refused requests may have landed.
	want := &ateapipb.WorkerResources{Actors: 1, Resources: resources.CPUMemory(1000, 1<<30)}
	if diff := cmp.Diff(want, workerAllocated(t, st), protocmp.Transform()); diff != "" {
		t.Errorf("allocated changed despite every request being refused (-want +got):\n%s", diff)
	}
}
