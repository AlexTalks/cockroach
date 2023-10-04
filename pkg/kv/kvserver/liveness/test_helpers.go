// Copyright 2023 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package liveness

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/cockroach/pkg/kv/kvserver/liveness/livenesspb"
	"github.com/cockroachdb/cockroach/pkg/roachpb"
)

const (
	// TestTimeUntilNodeDead is the test value for TimeUntilNodeDead to quickly
	// mark stores as dead. This needs to be longer than gossip.StoresInterval
	TestTimeUntilNodeDead = 15 * time.Second

	// TestTimeUntilNodeDeadOff is the test value for TimeUntilNodeDead that
	// prevents the store pool from marking stores as dead.
	TestTimeUntilNodeDeadOff = 24 * time.Hour
)

// TestStorageWrapper provides a simple liveness.Storage implementation for
// which mock implementations can be supplied by tests.
type TestStorageWrapper struct {
	GetImpl    func(ctx context.Context, nodeID roachpb.NodeID) (Record, error)
	UpdateImpl func(ctx context.Context, update LivenessUpdate, handleCondFailed func(actual Record) error) (Record, error)
	CreateImpl func(ctx context.Context, nodeID roachpb.NodeID) error
	ScanImpl   func(ctx context.Context) ([]Record, error)
}

var _ Storage = (*TestStorageWrapper)(nil)

func (s TestStorageWrapper) Get(
	ctx context.Context, nodeID roachpb.NodeID,
) (Record, error) {
	return s.GetImpl(ctx, nodeID)
}

func (s TestStorageWrapper) Update(
	ctx context.Context,
	update LivenessUpdate,
	handleCondFailed func(actual Record) error,
) (Record, error) {
	return s.UpdateImpl(ctx, update, handleCondFailed)
}

func (s TestStorageWrapper) Create(ctx context.Context, nodeID roachpb.NodeID) error {
	return s.CreateImpl(ctx, nodeID)
}

func (s TestStorageWrapper) Scan(ctx context.Context) ([]Record, error) {
	return s.ScanImpl(ctx)
}

// PauseHeartbeatLoopForTest stops the periodic heartbeat. The function
// waits until it acquires the heartbeatToken (unless heartbeat was
// already paused); this ensures that no heartbeats happen after this is
// called. Returns a closure to call to re-enable the heartbeat loop.
// This function is only safe for use in tests.
func (nl *NodeLiveness) PauseHeartbeatLoopForTest() func() {
	if swapped := atomic.CompareAndSwapUint32(&nl.heartbeatPaused, 0, 1); swapped {
		<-nl.heartbeatToken
	}
	return func() {
		if swapped := atomic.CompareAndSwapUint32(&nl.heartbeatPaused, 1, 0); swapped {
			nl.heartbeatToken <- struct{}{}
		}
	}
}

// PauseSynchronousHeartbeatsForTest disables all node liveness
// heartbeats triggered from outside the normal Start loop.
// Returns a closure to call to re-enable synchronous heartbeats. Only
// safe for use in tests.
func (nl *NodeLiveness) PauseSynchronousHeartbeatsForTest() func() {
	nl.selfSem <- struct{}{}
	nl.otherSem <- struct{}{}
	return func() {
		<-nl.selfSem
		<-nl.otherSem
	}
}

// PauseAllHeartbeatsForTest disables all node liveness heartbeats,
// including those triggered from outside the normal Start
// loop. Returns a closure to call to re-enable heartbeats. Only safe
// for use in tests.
func (nl *NodeLiveness) PauseAllHeartbeatsForTest() func() {
	enableLoop := nl.PauseHeartbeatLoopForTest()
	enableSync := nl.PauseSynchronousHeartbeatsForTest()
	return func() {
		enableLoop()
		enableSync()
	}
}

// TestingSetDrainingInternal is a testing helper to set the internal draining
// state for a NodeLiveness instance.
func (nl *NodeLiveness) TestingSetDrainingInternal(
	ctx context.Context, liveness Record, drain bool,
) error {
	return nl.setDrainingInternal(ctx, liveness, drain, nil /* reporter */)
}

// TestingSetDecommissioningInternal is a testing helper to set the internal
// decommissioning state for a NodeLiveness instance.
func (nl *NodeLiveness) TestingSetDecommissioningInternal(
	ctx context.Context, oldLivenessRec Record, targetStatus livenesspb.MembershipStatus,
) (changeCommitted bool, err error) {
	return nl.setMembershipStatusInternal(ctx, oldLivenessRec, targetStatus)
}

// TestingMaybeUpdate replaces the liveness (if it appears newer) and invokes
// the registered callbacks if the node became live in the process. For testing.
func (nl *NodeLiveness) TestingMaybeUpdate(ctx context.Context, newRec Record) {
	nl.cache.maybeUpdate(ctx, newRec)
}

// TestingGetLivenessThreshold returns the maximum duration between heartbeats
// before a node is considered not-live.
func (nl *NodeLiveness) TestingGetLivenessThreshold() time.Duration {
	return nl.livenessThreshold
}

// TestingOverrideStorage sets an overridden storage interface for use in
// persisting liveness. Allows for tests to mock storage and return values.
func (nl *NodeLiveness) TestingOverrideStorage(override Storage) {
	if nl.started.Get() {
		panic("liveness storage override is only permitted before start")
	}

	nl.storage = override
}
