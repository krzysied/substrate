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

package resources

import (
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/google/go-cmp/cmp"
)

func assignment(uid string, cpu, mem int64) *ateapipb.ActorAssignment {
	return &ateapipb.ActorAssignment{
		Actor:     &ateapipb.ObjectRef{Atespace: "demo", Name: uid},
		ActorUid:  uid,
		Resources: CPUMemory(cpu, mem),
	}
}

// mustAdd is AddToAllocated where the fixtures are known to parse.
func mustAdd(t *testing.T, total *ateapipb.WorkerResources, a *ateapipb.ActorAssignment, sign int64) *ateapipb.WorkerResources {
	t.Helper()
	got, err := AddToAllocated(total, a, sign)
	if err != nil {
		t.Fatalf("AddToAllocated(%v, %d): %v", a, sign, err)
	}
	return got
}

// The total moves by one assignment's worth in each direction, and a Worker
// back to holding nothing carries no allocation at all rather than a zeroed
// message: emptied and never-filled have to be the same record.
func TestAddToAllocated(t *testing.T) {
	var total *ateapipb.WorkerResources
	total = mustAdd(t, total, assignment("a", 1000, 1<<30), +1)
	total = mustAdd(t, total, assignment("b", 500, 2<<30), +1)

	want := &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(1500, 3<<30)}
	if diff := cmp.Diff(want, total, protocmp.Transform()); diff != "" {
		t.Errorf("allocated mismatch (-want +got):\n%s", diff)
	}

	total = mustAdd(t, total, assignment("b", 500, 2<<30), -1)
	want = &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(1000, 1<<30)}
	if diff := cmp.Diff(want, total, protocmp.Transform()); diff != "" {
		t.Errorf("allocated after release mismatch (-want +got):\n%s", diff)
	}

	if total = mustAdd(t, total, assignment("a", 1000, 1<<30), -1); total != nil {
		t.Errorf("allocated = %v for a Worker holding nothing, want nil", total)
	}
}

// An Actor that declared no limits reserves nothing but still costs a slot.
func TestAddToAllocatedCountsAnActorWithoutResources(t *testing.T) {
	total := mustAdd(t, nil, &ateapipb.ActorAssignment{ActorUid: "a"}, +1)
	want := &ateapipb.WorkerResources{Actors: 1}
	if diff := cmp.Diff(want, total, protocmp.Transform()); diff != "" {
		t.Errorf("allocated mismatch (-want +got):\n%s", diff)
	}
}

// A quantity that will not parse is refused rather than counted as nothing:
// silently booking zero would overcommit the Worker.
func TestAddToAllocatedRejectsAnUnparseableQuantity(t *testing.T) {
	bad := &ateapipb.ActorAssignment{
		ActorUid:  "a",
		Resources: &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: "cpu", Quantity: "two"}}},
	}
	if _, err := AddToAllocated(nil, bad, +1); err == nil {
		t.Fatal("AddToAllocated() = nil error for an unparseable quantity, want one")
	}
}

// SumAllocated rebuilds what AddToAllocated adjusts; the contract tests hold the
// two to each other, so they have to agree on the empty case as well.
func TestSumAllocated(t *testing.T) {
	got, err := SumAllocated(nil)
	if err != nil {
		t.Fatalf("SumAllocated(nil): %v", err)
	}
	if got != nil {
		t.Errorf("SumAllocated(nil) = %v, want nil", got)
	}

	got, err = SumAllocated([]*ateapipb.ActorAssignment{assignment("a", 1000, 1<<30), assignment("b", 500, 0)})
	if err != nil {
		t.Fatalf("SumAllocated(): %v", err)
	}
	want := &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(1500, 1<<30)}
	if diff := cmp.Diff(want, got, protocmp.Transform()); diff != "" {
		t.Errorf("SumAllocated() mismatch (-want +got):\n%s", diff)
	}
}

// Under overcommit a release frees the slot but leaves resources to the
// Worker, and a Worker holding nothing still carries no allocation at all.
func TestAddActorCountOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total *ateapipb.WorkerResources
		delta int32
		want  *ateapipb.WorkerResources
	}{
		{
			name:  "release keeps resources",
			total: &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(1500, 3<<30)},
			delta: -1,
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(1500, 3<<30)},
		},
		{
			name:  "increment",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(1500, 3<<30)},
			delta: +1,
			want:  &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(1500, 3<<30)},
		},
		{
			name:  "count clamps at zero",
			total: &ateapipb.WorkerResources{Resources: CPUMemory(100, 0)},
			delta: -1,
			want:  &ateapipb.WorkerResources{Resources: CPUMemory(100, 0)},
		},
		{
			name:  "emptied is nil",
			total: &ateapipb.WorkerResources{Actors: 1},
			delta: -1,
			want:  nil,
		},
		{
			name:  "nil stays nil",
			total: nil,
			delta: -1,
			want:  nil,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := AddActorCountOnly(tc.total, tc.delta)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("AddActorCountOnly() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// badDeltas are refused by both AddResourcesToAllocated and
// SubtractResourcesFromAllocated.
var badDeltas = []struct {
	name  string
	total *ateapipb.WorkerResources
	delta *ateapipb.Resources
}{
	{
		name:  "negative quantity",
		total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 0)},
		delta: &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: ResourceCPU, Quantity: "-100m"}}},
	},
	{
		name:  "unparseable delta",
		total: &ateapipb.WorkerResources{Actors: 1},
		delta: &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: ResourceCPU, Quantity: "two"}}},
	},
	{
		name:  "unparseable allocation",
		total: &ateapipb.WorkerResources{Actors: 1, Resources: &ateapipb.Resources{Limits: []*ateapipb.Limits{{Name: ResourceCPU, Quantity: "two"}}}},
		delta: CPUMemory(1, 0),
	},
}

func TestAddResourcesToAllocated(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total *ateapipb.WorkerResources
		delta *ateapipb.Resources
		want  *ateapipb.WorkerResources
	}{
		{
			name:  "grow",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 1<<30)},
			delta: CPUMemory(250, 1<<30),
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(750, 2<<30)},
		},
		{
			name:  "grow a new dimension",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 0)},
			delta: CPUMemory(0, 1<<30),
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 1<<30)},
		},
		{
			name:  "grow from nothing",
			total: nil,
			delta: CPUMemory(250, 0),
			want:  &ateapipb.WorkerResources{Resources: CPUMemory(250, 0)},
		},
		{
			name:  "empty delta",
			total: &ateapipb.WorkerResources{Actors: 3, Resources: CPUMemory(500, 0)},
			delta: nil,
			want:  &ateapipb.WorkerResources{Actors: 3, Resources: CPUMemory(500, 0)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := AddResourcesToAllocated(tc.total, tc.delta)
			if err != nil {
				t.Fatalf("AddResourcesToAllocated(): %v", err)
			}
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("AddResourcesToAllocated() mismatch (-want +got):\n%s", diff)
			}
		})
	}
	for _, tc := range badDeltas {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := AddResourcesToAllocated(tc.total, tc.delta); err == nil {
				t.Fatalf("AddResourcesToAllocated() = %v, want an error", got)
			}
		})
	}
}

func TestSubtractResourcesFromAllocated(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total *ateapipb.WorkerResources
		delta *ateapipb.Resources
		want  *ateapipb.WorkerResources
	}{
		{
			name:  "reclaim",
			total: &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(2000, 4<<30)},
			delta: CPUMemory(1500, 1<<30),
			want:  &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(500, 3<<30)},
		},
		{
			name:  "one dimension clamps at zero, the other stays",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 1<<30)},
			delta: CPUMemory(900, 512<<20),
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(0, 512<<20)},
		},
		{
			name:  "exactly to zero",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 1<<30)},
			delta: CPUMemory(500, 0),
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(0, 1<<30)},
		},
		{
			name:  "absent dimension stays absent",
			total: &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 0)},
			delta: CPUMemory(0, 1<<30),
			want:  &ateapipb.WorkerResources{Actors: 1, Resources: CPUMemory(500, 0)},
		},
		{
			name:  "from nothing stays nothing",
			total: nil,
			delta: CPUMemory(500, 1<<30),
			want:  nil,
		},
		{
			name:  "emptied with no actors is nil",
			total: &ateapipb.WorkerResources{Resources: CPUMemory(500, 1<<30)},
			delta: CPUMemory(500, 2<<30),
			want:  nil,
		},
		{
			name:  "emptied with actors keeps the count",
			total: &ateapipb.WorkerResources{Actors: 2, Resources: CPUMemory(500, 1<<30)},
			delta: CPUMemory(9000, 9<<30),
			want:  &ateapipb.WorkerResources{Actors: 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SubtractResourcesFromAllocated(tc.total, tc.delta)
			if err != nil {
				t.Fatalf("SubtractResourcesFromAllocated(): %v", err)
			}
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("SubtractResourcesFromAllocated() mismatch (-want +got):\n%s", diff)
			}
			held, err := ParseQuantities(got.GetResources())
			if err != nil {
				t.Fatalf("ParseQuantities(result): %v", err)
			}
			for name, quantity := range held {
				if quantity.Sign() < 0 {
					t.Errorf("result holds %s = %s, want never negative", name, quantity.String())
				}
			}
		})
	}
	for _, tc := range badDeltas {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := SubtractResourcesFromAllocated(tc.total, tc.delta); err == nil {
				t.Fatalf("SubtractResourcesFromAllocated() = %v, want an error", got)
			}
		})
	}
}
