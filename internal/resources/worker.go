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
	"fmt"
	"maps"
	"slices"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
)

// AddToAllocated adjusts allocation by an assignment; sign is 1 or -1.
// It returns nil when allocation reaches zero.
func AddToAllocated(total *ateapipb.WorkerResources, assignment *ateapipb.ActorAssignment, sign int64) (*ateapipb.WorkerResources, error) {
	held, err := ParseQuantities(total.GetResources())
	if err != nil {
		return nil, fmt.Errorf("allocated: %w", err)
	}
	booked, err := ParseQuantities(assignment.GetResources())
	if err != nil {
		return nil, fmt.Errorf("assignment for actor %s: %w", assignment.GetActorUid(), err)
	}
	if held == nil {
		held = Quantities{}
	}
	if sign > 0 {
		held.Add(booked)
	} else {
		held.Sub(booked)
	}

	actors := total.GetActors() + int32(sign)
	resources := held.Proto()
	if actors == 0 && resources == nil {
		return nil, nil
	}
	return &ateapipb.WorkerResources{Actors: actors, Resources: resources}, nil
}

// AddActorCountOnly adjusts the actor count by deltaActors and leaves resources
// untouched. Under worker-level overcommit a released Actor's resources are
// handed back by the Worker rather than by the release, so only its slot moves
// here. The count is clamped at zero. It returns nil when allocation reaches
// zero.
func AddActorCountOnly(total *ateapipb.WorkerResources, deltaActors int32) *ateapipb.WorkerResources {
	actors := max(total.GetActors()+deltaActors, 0)
	resources := total.GetResources()
	if actors == 0 && len(resources.GetLimits()) == 0 {
		return nil
	}
	return &ateapipb.WorkerResources{Actors: actors, Resources: resources}
}

// AddResourcesToAllocated adds delta to the allocated resources and leaves the
// actor count untouched. Every quantity in delta must be non-negative. It
// returns nil when allocation reaches zero.
func AddResourcesToAllocated(total *ateapipb.WorkerResources, delta *ateapipb.Resources) (*ateapipb.WorkerResources, error) {
	held, d, err := parseAllocationDelta(total, delta)
	if err != nil {
		return nil, err
	}
	held.Add(d)
	return withResources(total.GetActors(), held), nil
}

// SubtractResourcesFromAllocated subtracts delta from the allocated resources
// and leaves the actor count untouched. Every quantity in delta must be
// non-negative. Each dimension is clamped at zero, so a Worker handing back
// more than is booked empties that dimension instead of driving it negative.
// It returns nil when allocation reaches zero.
func SubtractResourcesFromAllocated(total *ateapipb.WorkerResources, delta *ateapipb.Resources) (*ateapipb.WorkerResources, error) {
	held, d, err := parseAllocationDelta(total, delta)
	if err != nil {
		return nil, err
	}
	for name, quantity := range d {
		existing, ok := held[name]
		if !ok {
			continue // nothing held to subtract from
		}
		existing.Sub(quantity)
		if existing.Sign() <= 0 {
			delete(held, name)
			continue
		}
		held[name] = existing
	}
	return withResources(total.GetActors(), held), nil
}

// parseAllocationDelta parses the held allocation and a delta, refusing a
// delta with a negative quantity: direction comes from which helper is called,
// never from the sign of a quantity.
func parseAllocationDelta(total *ateapipb.WorkerResources, delta *ateapipb.Resources) (Quantities, Quantities, error) {
	held, err := ParseQuantities(total.GetResources())
	if err != nil {
		return nil, nil, fmt.Errorf("allocated: %w", err)
	}
	d, err := ParseQuantities(delta)
	if err != nil {
		return nil, nil, fmt.Errorf("delta: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(d)) {
		if quantity := d[name]; quantity.Sign() < 0 {
			return nil, nil, fmt.Errorf("delta %s is %s, must not be negative", name, quantity.String())
		}
	}
	if held == nil {
		held = Quantities{}
	}
	return held, d, nil
}

// withResources builds an allocation, or nil when it holds nothing.
func withResources(actors int32, held Quantities) *ateapipb.WorkerResources {
	resources := held.Proto()
	if actors == 0 && resources == nil {
		return nil
	}
	return &ateapipb.WorkerResources{Actors: actors, Resources: resources}
}

// SumAllocated is what a set of assignments takes from a Worker, or nil for
// none. Rebuilds the total rather than adjusting it, which is what the checks
// holding AddToAllocated to the assignments it counts compare against.
func SumAllocated(assignments []*ateapipb.ActorAssignment) (*ateapipb.WorkerResources, error) {
	if len(assignments) == 0 {
		return nil, nil
	}
	total := Quantities{}
	for _, assignment := range assignments {
		booked, err := ParseQuantities(assignment.GetResources())
		if err != nil {
			return nil, fmt.Errorf("assignment for actor %s: %w", assignment.GetActorUid(), err)
		}
		total.Add(booked)
	}
	return &ateapipb.WorkerResources{Actors: int32(len(assignments)), Resources: total.Proto()}, nil
}
