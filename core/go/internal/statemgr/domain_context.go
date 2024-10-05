// Copyright © 2024 Kaleido, Inc.
//
// SPDX-License-Identifier: Apache-2.0
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

package statemgr

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/hyperledger/firefly-common/pkg/i18n"
	"github.com/kaleido-io/paladin/core/internal/components"
	"github.com/kaleido-io/paladin/core/internal/filters"
	"github.com/kaleido-io/paladin/core/internal/msgs"

	"github.com/kaleido-io/paladin/toolkit/pkg/log"
	"github.com/kaleido-io/paladin/toolkit/pkg/query"
	"github.com/kaleido-io/paladin/toolkit/pkg/tktypes"
)

type domainContext struct {
	id              uuid.UUID
	ss              *stateManager
	domainName      string
	contractAddress tktypes.EthAddress
	stateLock       sync.Mutex
	unFlushed       *writeOperation
	flushing        *writeOperation
	domainContexts  map[uuid.UUID]*domainContext
	closed          bool

	// We track creatingStates states beyond the flush - until the transaction that created them is removed, or a full reset
	// This is because the DB will never return them as "available"
	creatingStates map[string]*components.StateWithLabels

	// State locks are an in memory structure only, recording a set of locks associated with each transaction.
	// These are held only in memory, and used during DB queries to create a view on top of the database
	// that can make both additional states available, and remove visibility to states.
	txLocks []*components.StateLock
}

// Very important that callers Close domain contexts they open
func (ss *stateManager) NewDomainContext(ctx context.Context, domainName string, contractAddress tktypes.EthAddress) components.DomainContext {
	id := uuid.New()
	log.L(ctx).Debugf("Domain context %s for domain %s contract %s closed", id, domainName, contractAddress)

	ss.domainContextLock.Lock()
	defer ss.domainContextLock.Unlock()

	dc := &domainContext{
		id:              id,
		ss:              ss,
		domainName:      domainName,
		contractAddress: contractAddress,
		creatingStates:  make(map[string]*components.StateWithLabels),
		domainContexts:  make(map[uuid.UUID]*domainContext),
	}
	ss.domainContexts[id] = dc
	return dc
}

// nil if not found
func (ss *stateManager) GetDomainContext(ctx context.Context, id uuid.UUID) components.DomainContext {
	ss.domainContextLock.Lock()
	defer ss.domainContextLock.Unlock()

	return ss.domainContexts[id]
}

func (ss *stateManager) ListDomainContext() []components.DomainContextInfo {
	ss.domainContextLock.Lock()
	defer ss.domainContextLock.Unlock()

	dcs := make([]components.DomainContextInfo, 0, len(ss.domainContexts))
	for _, dc := range ss.domainContexts {
		dcs = append(dcs, dc.Info())
	}
	return dcs

}

func (dc *domainContext) getUnFlushedStates(ctx context.Context) (spending []tktypes.HexBytes, spent []tktypes.HexBytes, nullifiers []*components.StateNullifier, err error) {
	// Take lock and check flush state
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()
	if flushErr := dc.checkResetInitUnFlushed(ctx); flushErr != nil {
		return nil, nil, nil, flushErr
	}

	for _, l := range dc.txLocks {
		if l.Type.V() == components.StateLockTypeSpend {
			spending = append(spending, l.State)
		}
	}
	nullifiers = append(nullifiers, dc.unFlushed.stateNullifiers...)
	if dc.flushing != nil {
		nullifiers = append(nullifiers, dc.flushing.stateNullifiers...)
	}
	return spending, spent, nullifiers, nil
}

func (dc *domainContext) mergeUnFlushedApplyLocks(ctx context.Context, schema components.Schema, dbStates []*components.State, query *query.QueryJSON, requireNullifier bool) (_ []*components.State, err error) {
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()
	if flushErr := dc.checkResetInitUnFlushed(ctx); flushErr != nil {
		return nil, flushErr
	}

	// Get the list of new un-flushed states, which are not already locked for spend
	matches := make([]*components.StateWithLabels, 0, len(dc.unFlushed.states))
	schemaId := schema.Persisted().ID
	for _, state := range dc.creatingStates {
		if !state.Schema.Equals(&schemaId) {
			continue
		}
		spent := false
		for _, lock := range dc.txLocks {
			if lock.State.Equals(state.ID) && lock.Type.V() == components.StateLockTypeSpend {
				spent = true
				break
			}
		}
		// Cannot return it if it's spent or locked for spending
		if spent {
			continue
		}

		if requireNullifier && state.Nullifier == nil {
			continue
		}

		// Now we see if it matches the query
		labelSet := dc.ss.labelSetFor(schema)
		match, err := filters.EvalQuery(ctx, query, labelSet, state.LabelValues)
		if err != nil {
			return nil, err
		}
		if match {
			dup := false
			for _, dbState := range dbStates {
				if dbState.ID.Equals(state.ID) {
					dup = true
					break
				}
			}
			if !dup {
				log.L(ctx).Debugf("Matched state %s from un-flushed writes", &state.ID)
				// Take a shallow copy, as we'll apply the locks as they exist right now
				shallowCopy := *state
				matches = append(matches, &shallowCopy)
			}
		}
	}

	retStates := dbStates
	if len(matches) > 0 {
		// Build the merged list - this involves extra cost, as we deliberately don't reconstitute
		// the labels in JOIN on DB load (affecting every call at the DB side), instead we re-parse
		// them as we need them
		if retStates, err = dc.mergeInMemoryMatches(ctx, schema, dbStates, matches, query); err != nil {
			return nil, err
		}
	}

	return dc.applyLocks(retStates), nil
}

func (dc *domainContext) Info() components.DomainContextInfo {
	return components.DomainContextInfo{
		ID:              dc.id,
		Domain:          dc.domainName,
		ContractAddress: dc.contractAddress,
	}
}

func (dc *domainContext) mergeInMemoryMatches(ctx context.Context, schema components.Schema, states []*components.State, extras []*components.StateWithLabels, query *query.QueryJSON) (_ []*components.State, err error) {

	// Reconstitute the labels for all the loaded states into the front of an aggregate list
	fullList := make([]*components.StateWithLabels, len(states), len(states)+len(extras))
	persistedStateIDs := make(map[string]bool)
	for i, s := range states {
		if fullList[i], err = schema.RecoverLabels(ctx, s); err != nil {
			return nil, err
		}
		persistedStateIDs[s.ID.String()] = true
	}

	// Copy the matches to the end of that same list
	// However, we can't be certain that some of the states that were in the flushing list, haven't made it
	// to the DB yet - so we do need to de-dup here.
	for _, s := range extras {
		if !persistedStateIDs[s.ID.String()] {
			fullList = append(fullList, s)
		}
	}

	// Sort it in place - note we ensure we always have a sort instruction on the DB
	sortInstructions := query.Sort
	if err = filters.SortValueSetInPlace(ctx, dc.ss.labelSetFor(schema), fullList, sortInstructions...); err != nil {
		return nil, err
	}

	// We only want the states (not the labels needed during sort),
	// and only up to the limit that might have been breached adding in our in-memory states
	len := len(fullList)
	if query.Limit != nil && len > *query.Limit {
		len = *query.Limit
	}
	retList := make([]*components.State, len)
	for i := 0; i < len; i++ {
		retList[i] = fullList[i].State
	}
	return retList, nil

}

func (dc *domainContext) FindAvailableStates(ctx context.Context, schemaID tktypes.Bytes32, query *query.QueryJSON) (components.Schema, []*components.State, error) {

	// Build a list of spending states
	spending, spent, _, err := dc.getUnFlushedStates(ctx)
	if err != nil {
		return nil, nil, err
	}
	spending = append(spending, spent...)

	// Run the query against the DB
	schema, states, err := dc.ss.findStates(ctx, dc.domainName, dc.contractAddress, schemaID, query, StateStatusAvailable, spending...)
	if err != nil {
		return nil, nil, err
	}

	// Merge in un-flushed states to results
	states, err = dc.mergeUnFlushedApplyLocks(ctx, schema, states, query, false)
	if err != nil {
		return nil, nil, err
	}

	return schema, states, err
}

func (dc *domainContext) FindAvailableNullifiers(ctx context.Context, schemaID tktypes.Bytes32, query *query.QueryJSON) (components.Schema, []*components.State, error) {

	// Build a list of unflushed and spending nullifiers
	spending, spent, nullifiers, err := dc.getUnFlushedStates(ctx)
	if err != nil {
		return nil, nil, err
	}
	statesWithNullifiers := make([]tktypes.HexBytes, len(nullifiers))
	for i, n := range nullifiers {
		statesWithNullifiers[i] = n.State
	}

	// Run the query against the DB
	schema, states, err := dc.ss.findAvailableNullifiers(ctx, dc.domainName, dc.contractAddress, schemaID, query, statesWithNullifiers, spending, spent)
	if err != nil {
		return nil, nil, err
	}

	// Attach nullifiers to states
	for _, s := range states {
		if s.Nullifier == nil {
			for _, n := range nullifiers {
				if n.State.Equals(s.ID) {
					s.Nullifier = n
					break
				}
			}
		}
	}

	// Merge in un-flushed states to results
	states, err = dc.mergeUnFlushedApplyLocks(ctx, schema, states, query, true)
	return schema, states, err
}

func (dc *domainContext) UpsertStates(ctx context.Context, stateUpserts ...*components.StateUpsert) (states []*components.State, err error) {

	states = make([]*components.State, len(stateUpserts))
	stateLocks := make([]*components.StateLock, 0, len(stateUpserts))
	withValues := make([]*components.StateWithLabels, len(stateUpserts))
	toMakeAvailable := make([]*components.StateWithLabels, 0, len(stateUpserts))
	for i, ns := range stateUpserts {
		schema, err := dc.ss.GetSchema(ctx, dc.domainName, ns.SchemaID, true)
		if err != nil {
			return nil, err
		}

		vs, err := schema.ProcessState(ctx, dc.contractAddress, ns.Data, ns.ID)
		if err != nil {
			return nil, err
		}
		withValues[i] = vs
		states[i] = withValues[i].State
		if ns.CreatedBy != nil {
			createLock := &components.StateLock{
				Type:        components.StateLockTypeCreate.Enum(),
				Transaction: *ns.CreatedBy,
				State:       withValues[i].State.ID,
			}
			stateLocks = append(stateLocks, createLock)
			toMakeAvailable = append(toMakeAvailable, vs)
			log.L(ctx).Infof("Upserting state %s with create lock tx=%s", states[i].ID, ns.CreatedBy)
		} else {
			log.L(ctx).Infof("Upserting state %s (no create lock)", states[i].ID)
		}
	}

	// Take lock and check flush state
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()
	if flushErr := dc.checkResetInitUnFlushed(ctx); flushErr != nil {
		return nil, flushErr
	}

	// We need to de-duplicate out any previous un-flushed state writes of the same ID
	deDuppedUnFlushedStates := make([]*components.StateWithLabels, 0, len(dc.unFlushed.states))
	for _, existing := range dc.unFlushed.states {
		var replaced bool
		for _, s := range withValues {
			if existing.ID.Equals(s.ID) {
				replaced = true
				break
			}
		}
		if !replaced {
			deDuppedUnFlushedStates = append(deDuppedUnFlushedStates, existing)
		}
	}
	// Now we can add our own un-flushed writes to the de-duplicated lists
	dc.unFlushed.states = append(deDuppedUnFlushedStates, withValues...)
	// Only those transactions with a creating TX lock can be returned from queries
	// (any other states supplied for flushing are just to ensure we have a copy of the state
	// for data availability when the existing/later confirm is available)
	for _, s := range toMakeAvailable {
		dc.creatingStates[s.ID.String()] = s
	}
	err = dc.addStateLocks(ctx, stateLocks...)
	if err != nil {
		return nil, err
	}
	return states, nil
}

func (dc *domainContext) UpsertNullifiers(ctx context.Context, nullifiers ...*components.StateNullifier) error {
	// Take lock and check flush state
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()
	if flushErr := dc.checkResetInitUnFlushed(ctx); flushErr != nil {
		return flushErr
	}

	dc.unFlushed.stateNullifiers = append(dc.unFlushed.stateNullifiers, nullifiers...)

	for _, nullifier := range nullifiers {
		creatingState := dc.creatingStates[nullifier.State.String()]
		if creatingState == nil {
			return i18n.NewError(ctx, msgs.MsgStateNullifierStateNotInCtx, nullifier.State, nullifier.ID)
		} else if creatingState.Nullifier != nil && !creatingState.Nullifier.ID.Equals(nullifier.ID) {
			return i18n.NewError(ctx, msgs.MsgStateNullifierConflict, nullifier.State, creatingState.Nullifier.ID)
		}
		creatingState.Nullifier = nullifier
	}

	return nil
}

func (dc *domainContext) addStateLocks(ctx context.Context, locks ...*components.StateLock) error {
	for _, l := range locks {
		if l.Transaction == (uuid.UUID{}) {
			return i18n.NewError(ctx, msgs.MsgStateLockNoTransaction)
		} else if len(l.State) == 0 {
			return i18n.NewError(ctx, msgs.MsgStateLockNoState)
		}

		// For creating the state must be in our map (via Upsert) or we will fail to return it
		creatingState := dc.creatingStates[l.State.String()]
		if l.Type.V() == components.StateLockTypeCreate && creatingState == nil {
			return i18n.NewError(ctx, msgs.MsgStateLockCreateNotInContext, l.State)
		}

		// Note we do NOT check for conflicts on existing state locks
		log.L(ctx).Debugf("state %s adding %s lock tx=%s)", l.State, l.Type, l.Transaction)
		dc.txLocks = append(dc.txLocks, l)
	}
	return nil
}

func (dc *domainContext) applyLocks(states []*components.State) []*components.State {
	for _, s := range states {
		s.Locks = []*components.StateLock{}
		for _, l := range dc.txLocks {
			if l.State.Equals(s.ID) {
				s.Locks = append(s.Locks, l)
			}
		}
	}
	return states
}

func (dc *domainContext) AddStateLocks(ctx context.Context, locks ...*components.StateLock) (err error) {
	// Take lock and check flush state
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()
	if flushErr := dc.checkResetInitUnFlushed(ctx); flushErr != nil {
		return flushErr
	}

	return dc.addStateLocks(ctx, locks...)
}

// Clear all in-memory locks associated with individual transactions, because they are no longer needed/applicable
// Most likely because the state transitions have now been finalized.
//
// Note it's important that this occurs after the confirmation record of creation of a state is fully committed
// to the database, as the in-memory "creating" record for a state will be removed as part of this.
func (dc *domainContext) ClearTransactions(ctx context.Context, transactions ...uuid.UUID) {
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()

	newLocks := make([]*components.StateLock, 0)
	for _, lock := range dc.txLocks {
		skip := false
		for _, tx := range transactions {
			if lock.Transaction == tx {
				if lock.Type.V() == components.StateLockTypeCreate {
					// Clean up the creating record
					delete(dc.creatingStates, lock.State.String())
				}
				skip = true
				break
			}
		}
		if !skip {
			newLocks = append(newLocks, lock)
		}
	}
	dc.txLocks = newLocks
}

func (dc *domainContext) StateLocksByTransaction() map[uuid.UUID][]components.StateLock {
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()

	txLocksCopy := make(map[uuid.UUID][]components.StateLock)
	for _, l := range dc.txLocks {
		txLocksCopy[l.Transaction] = append(txLocksCopy[l.Transaction], *l)
	}
	return txLocksCopy
}

// Reset puts the world back to fresh - including completing any flush.
//
// Must be called after a flush error before the context can be used, as on a flush
// error the caller must reset their processing to the last point of consistency
// as they cannot trust in-memory state
//
// Note it does not cancel or check the status of any in-progress flush, as the
// things that are flushed are inert records in isolation.
// Reset instead is intended to be a boundary where the calling code knows explicitly
// that any state-locks and states that haven't reached a confirmed flush must
// be re-written into the DomainContext.
func (dc *domainContext) Reset(ctx context.Context) {
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()

	err := dc.clearExistingFlush(ctx)
	if err != nil {
		log.L(ctx).Warnf("Reset recovering from flush error: %s", err)
	}

	dc.creatingStates = make(map[string]*components.StateWithLabels)
	dc.flushing = nil
	dc.unFlushed = nil
	dc.txLocks = nil
}

func (dc *domainContext) Close(ctx context.Context) {
	dc.stateLock.Lock()
	dc.closed = true
	dc.stateLock.Unlock()

	log.L(ctx).Debugf("Domain context %s for domain %s contract %s closed", dc.id, dc.domainName, dc.contractAddress)

	dc.ss.domainContextLock.Lock()
	defer dc.ss.domainContextLock.Unlock()
	delete(dc.ss.domainContexts, dc.id)
}

func (dc *domainContext) clearExistingFlush(ctx context.Context) error {
	// If we are already flushing, then we wait for that flush while holding the lock
	// here - until we can queue up the next flush.
	// e.g. we only get one flush ahead
	if dc.flushing != nil {
		select {
		case <-dc.flushing.flushed:
		case <-ctx.Done():
			// The caller gave up on us, we cannot flush
			return i18n.NewError(ctx, msgs.MsgContextCanceled)
		}
		return dc.flushing.flushResult
	}
	return nil
}

func (dc *domainContext) InitiateFlush(ctx context.Context, asyncCallback func(err error)) error {
	dc.stateLock.Lock()
	defer dc.stateLock.Unlock()

	// Sync check if there's already an error
	if err := dc.clearExistingFlush(ctx); err != nil {
		return err
	}

	// Ok we're good to go async
	flushing := dc.unFlushed
	dc.flushing = flushing
	dc.unFlushed = nil
	// Always dispatch a routine for the callback
	// even if there's a nil flushing - meaning nothing to do
	if flushing != nil {
		flushing.flushed = make(chan struct{})
		dc.ss.writer.queue(ctx, flushing)
	}
	go dc.doFlush(ctx, asyncCallback, flushing)
	return nil
}

// MUST hold the lock to call this function
// Simply checks there isn't an un-cleared error that means the caller must reset.
func (dc *domainContext) checkResetInitUnFlushed(ctx context.Context) error {
	if dc.closed {
		return i18n.NewError(ctx, msgs.MsgStateDomainContextClosed)
	}
	// Peek if there's a broken flush that needs a reset
	if dc.flushing != nil {
		select {
		case <-dc.flushing.flushed:
			if dc.flushing.flushResult != nil {
				log.L(ctx).Errorf("flush %s failed - domain context must be reset", dc.flushing.id)
				return i18n.WrapError(ctx, dc.flushing.flushResult, msgs.MsgStateFlushFailedDomainReset, dc.domainName, dc.contractAddress)

			}
		default:
		}
	}
	if dc.unFlushed == nil {
		dc.unFlushed = dc.ss.writer.newWriteOp(dc.domainName, dc.contractAddress)
	}
	return nil
}

// MUST NOT hold the lock to call this function - instead pass in a list of all the
// unflushed writers (max 2 in practice) that need to be successful for this flush to
// be considered complete
func (dc *domainContext) doFlush(ctx context.Context, cb func(error), flushing *writeOperation) {
	var err error
	// We might have found by the time we got the lock to flush, there was nothing to do
	if flushing != nil {
		log.L(ctx).Debugf("waiting for flush %s", flushing.id)
		err = flushing.flush(ctx)
		flushing.flushResult = err // for any other routines the blocked waiting
		log.L(ctx).Debugf("flush %s completed (err=%v)", flushing.id, err)
		close(flushing.flushed)
	}
	cb(err)
}
