// Copyright 2014 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package kvserver

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/allocator"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/allocator/allocatorimpl"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/allocator/storepool"
	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/liveness/livenesspb"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
	"github.com/cockroachdb/cockroach/pkg/storage/enginepb"
	"github.com/cockroachdb/cockroach/pkg/testutils/gossiputil"
	"github.com/cockroachdb/cockroach/pkg/util/hlc"
	"github.com/cockroachdb/cockroach/pkg/util/leaktest"
	"github.com/cockroachdb/cockroach/pkg/util/log"
	"github.com/cockroachdb/cockroach/pkg/util/timeutil"
	"github.com/stretchr/testify/require"
	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/tracker"
)

const firstRangeID = roachpb.RangeID(1)

var simpleSpanConfig = roachpb.SpanConfig{
	NumReplicas: 1,
	Constraints: []roachpb.ConstraintsConjunction{
		{
			Constraints: []roachpb.Constraint{
				{Value: "a", Type: roachpb.Constraint_REQUIRED},
				{Value: "ssd", Type: roachpb.Constraint_REQUIRED},
			},
		},
	},
}

var singleStore = []*roachpb.StoreDescriptor{
	{
		StoreID: 1,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 1,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
}

var threeStores = []*roachpb.StoreDescriptor{
	{
		StoreID: 1,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 1,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 2,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 2,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 3,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 3,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
}

var twoDCStores = []*roachpb.StoreDescriptor{
	{
		StoreID: 1,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 1,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 2,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 2,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 3,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 3,
			Attrs:  roachpb.Attributes{Attrs: []string{"a"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 4,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 4,
			Attrs:  roachpb.Attributes{Attrs: []string{"b"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 5,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 5,
			Attrs:  roachpb.Attributes{Attrs: []string{"b"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
	{
		StoreID: 6,
		Attrs:   roachpb.Attributes{Attrs: []string{"ssd"}},
		Node: roachpb.NodeDescriptor{
			NodeID: 6,
			Attrs:  roachpb.Attributes{Attrs: []string{"b"}},
		},
		Capacity: roachpb.StoreCapacity{
			Capacity:     200,
			Available:    100,
			LogicalBytes: 100,
		},
	},
}

func constrainTo(numReplicas int, attr string) roachpb.SpanConfig {
	return roachpb.SpanConfig{
		NumReplicas: int32(numReplicas),
		Constraints: []roachpb.ConstraintsConjunction{
			{
				Constraints: []roachpb.Constraint{
					{Value: attr, Type: roachpb.Constraint_REQUIRED},
				},
			},
		},
	}
}

// TestAllocatorRebalanceTarget could help us to verify whether we'll rebalance
// to a target that we'll immediately remove.
func TestAllocatorRebalanceTarget(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)
	clock := hlc.NewClock(timeutil.NewManualTime(timeutil.Unix(0, 123)), time.Nanosecond /* maxOffset */)
	ctx := context.Background()
	stopper, g, _, a, _ := allocatorimpl.CreateTestAllocator(ctx, 5, false /* deterministic */)
	defer stopper.Stop(ctx)
	// We make 5 stores in this test -- 3 in the same datacenter, and 1 each in
	// 2 other datacenters. All of our replicas are distributed within these 3
	// datacenters. Originally, the stores that are all alone in their datacenter
	// are fuller than the other stores. If we didn't simulate RemoveVoter in
	// RebalanceVoter, we would try to choose store 2 or 3 as the target store
	// to make a rebalance. However, we would immediately remove the replica on
	// store 1 or 2 to retain the locality diversity.
	stores := []*roachpb.StoreDescriptor{
		{
			StoreID: 1,
			Node: roachpb.NodeDescriptor{
				NodeID: 1,
				Locality: roachpb.Locality{
					Tiers: []roachpb.Tier{
						{Key: "datacenter", Value: "a"},
					},
				},
			},
			Capacity: roachpb.StoreCapacity{
				RangeCount: 50,
			},
		},
		{
			StoreID: 2,
			Node: roachpb.NodeDescriptor{
				NodeID: 2,
				Locality: roachpb.Locality{
					Tiers: []roachpb.Tier{
						{Key: "datacenter", Value: "a"},
					},
				},
			},
			Capacity: roachpb.StoreCapacity{
				RangeCount: 55,
			},
		},
		{
			StoreID: 3,
			Node: roachpb.NodeDescriptor{
				NodeID: 3,
				Locality: roachpb.Locality{
					Tiers: []roachpb.Tier{
						{Key: "datacenter", Value: "a"},
					},
				},
			},
			Capacity: roachpb.StoreCapacity{
				RangeCount: 55,
			},
		},
		{
			StoreID: 4,
			Node: roachpb.NodeDescriptor{
				NodeID: 4,
				Locality: roachpb.Locality{
					Tiers: []roachpb.Tier{
						{Key: "datacenter", Value: "b"},
					},
				},
			},
			Capacity: roachpb.StoreCapacity{
				RangeCount: 100,
			},
		},
		{
			StoreID: 5,
			Node: roachpb.NodeDescriptor{
				NodeID: 5,
				Locality: roachpb.Locality{
					Tiers: []roachpb.Tier{
						{Key: "datacenter", Value: "c"},
					},
				},
			},
			Capacity: roachpb.StoreCapacity{
				RangeCount: 100,
			},
		},
	}
	sg := gossiputil.NewStoreGossiper(g)
	sg.GossipStores(stores, t)

	replicas := []roachpb.ReplicaDescriptor{
		{NodeID: 1, StoreID: 1, ReplicaID: 1},
		{NodeID: 4, StoreID: 4, ReplicaID: 4},
		{NodeID: 5, StoreID: 5, ReplicaID: 5},
	}
	repl := &Replica{RangeID: firstRangeID}

	repl.mu.Lock()
	repl.mu.state.Stats = &enginepb.MVCCStats{}
	repl.mu.Unlock()

	repl.loadStats = NewReplicaLoad(clock, nil)

	var rangeUsageInfo allocator.RangeUsageInfo

	status := &raft.Status{
		Progress: make(map[uint64]tracker.Progress),
	}
	status.Lead = 1
	status.RaftState = raft.StateLeader
	status.Commit = 10
	for _, replica := range replicas {
		status.Progress[uint64(replica.ReplicaID)] = tracker.Progress{
			Match: 10,
			State: tracker.StateReplicate,
		}
	}
	for i := 0; i < 10; i++ {
		result, _, details, ok := a.RebalanceVoter(
			ctx,
			roachpb.SpanConfig{},
			status,
			replicas,
			nil,
			rangeUsageInfo,
			storepool.StoreFilterThrottled,
			a.ScorerOptions(ctx),
		)
		if ok {
			t.Fatalf("expected no rebalance, but got target s%d; details: %s", result.StoreID, details)
		}
	}

	// Set up a second round of testing where the other two stores in the big
	// locality actually have fewer replicas, but enough that it still isn't worth
	// rebalancing to them. We create a situation where the replacement candidates
	// for s1 (i.e. s2 and s3) have an average of 48 replicas each (leading to an
	// overfullness threshold of 51, which is greater than the replica count of
	// s1).
	stores[1].Capacity.RangeCount = 48
	stores[2].Capacity.RangeCount = 48
	sg.GossipStores(stores, t)
	for i := 0; i < 10; i++ {
		target, _, details, ok := a.RebalanceVoter(
			ctx,
			roachpb.SpanConfig{},
			status,
			replicas,
			nil,
			rangeUsageInfo,
			storepool.StoreFilterThrottled,
			a.ScorerOptions(ctx),
		)
		if ok {
			t.Fatalf("expected no rebalance, but got target s%d; details: %s", target.StoreID, details)
		}
	}

	// Make sure rebalancing does happen if we drop just a little further down.
	stores[1].Capacity.RangeCount = 44
	sg.GossipStores(stores, t)
	for i := 0; i < 10; i++ {
		target, origin, details, ok := a.RebalanceVoter(
			ctx,
			roachpb.SpanConfig{},
			status,
			replicas,
			nil,
			rangeUsageInfo,
			storepool.StoreFilterThrottled,
			a.ScorerOptions(ctx),
		)
		expTo := stores[1].StoreID
		expFrom := stores[0].StoreID
		if !ok || target.StoreID != expTo || origin.StoreID != expFrom {
			t.Fatalf("%d: expected rebalance from either of %v to s%d, but got %v->%v; details: %s",
				i, expFrom, expTo, origin, target, details)
		}
	}
}

// TestAllocatorCheckRangeActionUprelicate validates the allocator's action and
// target for a range in a basic upreplication case using the replicate queue's
// `CheckRangeAction(..)`.
func TestAllocatorCheckRangeActionUprelicate(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	stopper, g, sp, a, _ := allocatorimpl.CreateTestAllocator(ctx, 10, false /* deterministic */)
	defer stopper.Stop(context.Background())

	gossiputil.NewStoreGossiper(g).GossipStores(twoDCStores, t)
	cfg := TestStoreConfig(nil)
	cfg.Gossip = g

	// Ensure that there are no usages of the underlying store pool.
	cfg.StorePool = nil

	firstStore := *twoDCStores[0]
	s := createTestStoreWithoutStart(ctx, t, stopper, testStoreOpts{createSystemRanges: true}, &cfg)
	s.Ident = &roachpb.StoreIdent{StoreID: firstStore.StoreID}
	rq := newReplicateQueue(s, a)

	firstRange := &roachpb.RangeDescriptor{
		RangeID: 1,
		InternalReplicas: []roachpb.ReplicaDescriptor{
			{NodeID: 2, StoreID: 2},
		},
	}

	storeIDsInB := []roachpb.StoreID{4, 5, 6}

	constrainToB3X := constrainTo(3, "b")

	// Validate that we need to upreplicate r1 to a node in "b".
	action, target, err := rq.CheckRangeAction(ctx, sp, firstRange, constrainToB3X)

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorAddVoter, action)
	require.Contains(t, storeIDsInB, target.StoreID)

	newReplica := roachpb.ReplicaDescriptor{NodeID: target.NodeID, StoreID: target.StoreID}
	firstRange.InternalReplicas = append(firstRange.InternalReplicas, newReplica)

	// Validate that we need to upreplicate r1 to another node in "b".
	action, target, err = rq.CheckRangeAction(ctx, sp, firstRange, constrainToB3X)

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorAddVoter, action)
	require.Contains(t, storeIDsInB, target.StoreID)

	newReplica = roachpb.ReplicaDescriptor{NodeID: target.NodeID, StoreID: target.StoreID}
	firstRange.InternalReplicas = append(firstRange.InternalReplicas, newReplica)

	// Determine the remaining node in "b".
	var remainingStoreID roachpb.StoreID
	for _, storeID := range storeIDsInB {
		if !firstRange.Replicas().HasReplicaOnNode(roachpb.NodeID(storeID)) {
			remainingStoreID = storeID
			break
		}
	}

	// Validate that we need to rebalance r1 from n2 to the final node in "b".
	action, target, err = rq.CheckRangeAction(ctx, sp, firstRange, constrainToB3X)

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorConsiderRebalance, action)
	// NB: For rebalance actions, the target is currently undetermined, but
	// should be the remaining node.
	require.Equal(t, roachpb.ReplicationTarget{}, target)

	// Simulate adding a replica on the remaining node in "b", without removing.
	newReplica = roachpb.ReplicaDescriptor{NodeID: roachpb.NodeID(remainingStoreID), StoreID: remainingStoreID}
	firstRange.InternalReplicas = append(firstRange.InternalReplicas, newReplica)

	// Validate that we need to remove r1 from the node in "a".
	action, target, err = rq.CheckRangeAction(ctx, sp, firstRange, constrainToB3X)

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorRemoveVoter, action)
	// NB: For removal actions, the target is currently undetermined, but
	// should be n2.
	require.Equal(t, roachpb.ReplicationTarget{}, target)

	removeIdx := getRemoveIdx(firstRange.InternalReplicas, roachpb.ReplicaDescriptor{StoreID: 2})
	firstRange.InternalReplicas = append(firstRange.InternalReplicas[:removeIdx:removeIdx],
		firstRange.InternalReplicas[removeIdx+1:]...)

	// Validate that we have no more actions on r1, except to consider rebalance.
	action, target, err = rq.CheckRangeAction(ctx, sp, firstRange, constrainToB3X)

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorConsiderRebalance, action)
}

// TestAllocatorCheckRangeActionProposedDecommissionSelf validates the allocator's action and
// target for a range during a proposed (but not current) decommission using the
// replicate queue's `CheckRangeAction(..)`.
func TestAllocatorCheckRangeActionProposedDecommissionSelf(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	stopper, g, sp, a, _ := allocatorimpl.CreateTestAllocator(ctx, 10, false /* deterministic */)
	defer stopper.Stop(context.Background())

	gossiputil.NewStoreGossiper(g).GossipStores(twoDCStores, t)
	cfg := TestStoreConfig(nil)
	cfg.Gossip = g

	// Ensure that there are no usages of the underlying store pool.
	cfg.StorePool = nil

	firstStore := *twoDCStores[0]
	s := createTestStoreWithoutStart(ctx, t, stopper, testStoreOpts{createSystemRanges: true}, &cfg)
	s.Ident = &roachpb.StoreIdent{StoreID: firstStore.StoreID}
	rq := newReplicateQueue(s, a)

	firstRange := &roachpb.RangeDescriptor{
		RangeID: 1,
		InternalReplicas: []roachpb.ReplicaDescriptor{
			{NodeID: 2, StoreID: 2},
			{NodeID: 3, StoreID: 3},
			{NodeID: 4, StoreID: 4},
		},
	}

	remainingStores := []roachpb.StoreID{5, 6}

	// Simulate n2 as decommissioning and n1 as down.
	override := storepool.NewOverrideStorePool(sp, func(nid roachpb.NodeID, now time.Time, timeUntilStoreDead time.Duration) livenesspb.NodeLivenessStatus {
		if nid == roachpb.NodeID(2) {
			return livenesspb.NodeLivenessStatus_DECOMMISSIONING
		} else if nid == roachpb.NodeID(1) {
			return livenesspb.NodeLivenessStatus_DEAD
		} else {
			return livenesspb.NodeLivenessStatus_LIVE
		}
	})

	// Validate that we need to do a decommissioning voter replacement for r1 to
	// a node in "b".
	action, target, err := rq.CheckRangeAction(ctx, override, firstRange, roachpb.SpanConfig{NumReplicas: 3})

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorReplaceDecommissioningVoter, action)
	require.Contains(t, remainingStores, target.StoreID)

	// Validate that we'd just need to remove n2's replica if we only need one
	// replica.
	action, target, err = rq.CheckRangeAction(ctx, override, firstRange, roachpb.SpanConfig{NumReplicas: 1})

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorRemoveDecommissioningVoter, action)
	// NB: For removal actions, the target is currently undetermined, but
	// should be n2.
	require.Equal(t, roachpb.ReplicationTarget{}, target)

	// Validate that we would get an error finding a target if we restrict r1 to
	// only "a" nodes, since n1 is down.
	constrainToA3X := constrainTo(3, "a")
	action, target, err = rq.CheckRangeAction(ctx, override, firstRange, constrainToA3X)

	require.Error(t, err)
	require.Equal(t, allocatorimpl.AllocatorReplaceDecommissioningVoter, action)
	require.Equal(t, roachpb.ReplicationTarget{}, target)

	// Validate that any other type of replica other than voter or non-voter on
	// n2 indicates that we must complete the atomic replication change prior to
	// handling the decommissioning replica.
	inChangeReplicaTypes := []roachpb.ReplicaType{
		roachpb.VOTER_INCOMING, roachpb.VOTER_OUTGOING,
		roachpb.VOTER_DEMOTING_LEARNER, roachpb.VOTER_DEMOTING_NON_VOTER,
	}
	for _, replicaType := range inChangeReplicaTypes {
		firstRange.InternalReplicas[0].Type = replicaType

		action, target, err = rq.CheckRangeAction(ctx, override, firstRange, roachpb.SpanConfig{NumReplicas: 3})
		require.NoError(t, err)
		require.Equal(t, allocatorimpl.AllocatorFinalizeAtomicReplicationChange, action)
		require.Equal(t, roachpb.ReplicationTarget{}, target)
	}

	// Simulate n2's and n3's replicas of r1 as a non-voter replicas.
	firstRange.InternalReplicas[0].Type = roachpb.NON_VOTER
	firstRange.InternalReplicas[1].Type = roachpb.NON_VOTER

	// Validate that we'd need to replace the n2's non-voting replica if we need
	// 3 replicas but only 1 voter.
	action, target, err = rq.CheckRangeAction(ctx, override, firstRange, roachpb.SpanConfig{NumReplicas: 3, NumVoters: 1})

	require.NoError(t, err)
	require.Equal(t, allocatorimpl.AllocatorReplaceDecommissioningNonVoter, action)
	require.Contains(t, remainingStores, target.StoreID)
}

// TestAllocatorThrottled ensures that when a store is throttled, the replica
// will not be sent to purgatory.
func TestAllocatorThrottled(t *testing.T) {
	defer leaktest.AfterTest(t)()
	defer log.Scope(t).Close(t)

	ctx := context.Background()
	stopper, g, _, a, _ := allocatorimpl.CreateTestAllocator(ctx, 10, false /* deterministic */)
	defer stopper.Stop(ctx)

	// First test to make sure we would send the replica to purgatory.
	_, _, err := a.AllocateVoter(ctx, simpleSpanConfig, []roachpb.ReplicaDescriptor{}, nil, allocatorimpl.Dead)
	if _, ok := IsPurgatoryError(err); !ok {
		t.Fatalf("expected a purgatory error, got: %+v", err)
	}

	// Second, test the normal case in which we can allocate to the store.
	gossiputil.NewStoreGossiper(g).GossipStores(singleStore, t)
	result, _, err := a.AllocateVoter(ctx, simpleSpanConfig, []roachpb.ReplicaDescriptor{}, nil, allocatorimpl.Dead)
	if err != nil {
		t.Fatalf("unable to perform allocation: %+v", err)
	}
	if result.NodeID != 1 || result.StoreID != 1 {
		t.Errorf("expected NodeID 1 and StoreID 1: %+v", result)
	}

	// Finally, set that store to be throttled and ensure we don't send the
	// replica to purgatory.
	storePool := a.StorePool.(*storepool.StorePool)
	storePool.DetailsMu.Lock()
	storeDetail, ok := storePool.DetailsMu.StoreDetails[singleStore[0].StoreID]
	if !ok {
		t.Fatalf("store:%d was not found in the store pool", singleStore[0].StoreID)
	}
	storeDetail.ThrottledUntil = timeutil.Now().Add(24 * time.Hour)
	storePool.DetailsMu.Unlock()
	_, _, err = a.AllocateVoter(ctx, simpleSpanConfig, []roachpb.ReplicaDescriptor{}, nil, allocatorimpl.Dead)
	if _, ok := IsPurgatoryError(err); ok {
		t.Fatalf("expected a non purgatory error, got: %+v", err)
	}
}
