/*
 * Copyright © 2024 Kaleido, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
 * the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on
 * an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package zeto

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/hyperledger-labs/zeto/go-sdk/pkg/crypto"
	"github.com/kaleido-io/paladin/domains/zeto/internal/zeto/smt"
	"github.com/kaleido-io/paladin/domains/zeto/pkg/types"
	"github.com/kaleido-io/paladin/domains/zeto/pkg/zetosigner"
	"github.com/kaleido-io/paladin/domains/zeto/pkg/zetosigner/zetosignerapi"
	"github.com/kaleido-io/paladin/toolkit/pkg/domain"
	pb "github.com/kaleido-io/paladin/toolkit/pkg/prototk"
	"github.com/kaleido-io/paladin/toolkit/pkg/query"
	"github.com/kaleido-io/paladin/toolkit/pkg/tktypes"
)

var MAX_INPUT_COUNT = 10
var MAX_OUTPUT_COUNT = 10

func getStateSchemas() ([]string, error) {
	var schemas []string
	coinJSON, err := json.Marshal(types.ZetoCoinABI)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Zeto Coin schema abi. %s", err)
	}
	schemas = append(schemas, string(coinJSON))

	smtRootJSON, err := json.Marshal(smt.MerkleTreeRootABI)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Merkle Tree Root schema abi. %s", err)
	}
	schemas = append(schemas, string(smtRootJSON))

	smtNodeJSON, err := json.Marshal(smt.MerkleTreeNodeABI)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Merkle Tree Node schema abi. %s", err)
	}
	schemas = append(schemas, string(smtNodeJSON))

	return schemas, nil
}

func (n *Zeto) makeCoin(stateData string) (*types.ZetoCoin, error) {
	coin := &types.ZetoCoin{}
	err := json.Unmarshal([]byte(stateData), &coin)
	return coin, err
}

func (z *Zeto) makeNewState(coin *types.ZetoCoin) (*pb.NewState, error) {
	coinJSON, err := json.Marshal(coin)
	if err != nil {
		return nil, err
	}
	hash, err := coin.Hash()
	if err != nil {
		return nil, err
	}
	hashStr := hash.String()
	return &pb.NewState{
		Id:            &hashStr,
		SchemaId:      z.coinSchema.Id,
		StateDataJson: string(coinJSON),
	}, nil
}

func (z *Zeto) prepareInputs(ctx context.Context, stateQueryContext, sender string, params []*types.TransferParamEntry) ([]*types.ZetoCoin, []*pb.StateRef, *big.Int, *big.Int, error) {
	var lastStateTimestamp int64
	total := big.NewInt(0)
	stateRefs := []*pb.StateRef{}
	coins := []*types.ZetoCoin{}

	expectedTotal := big.NewInt(0)
	for _, param := range params {
		expectedTotal = expectedTotal.Add(expectedTotal, param.Amount.Int())
	}

	for {
		queryBuilder := query.NewQueryBuilder().
			Limit(10).
			Sort(".created").
			Equal("owner", sender)

		if lastStateTimestamp > 0 {
			queryBuilder.GreaterThan(".created", lastStateTimestamp)
		}
		states, err := z.findAvailableStates(ctx, stateQueryContext, queryBuilder.Query().String())
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("failed to query the state store for available coins. %s", err)
		}
		if len(states) == 0 {
			return nil, nil, nil, nil, fmt.Errorf("insufficient funds (available=%s)", total.Text(10))
		}
		for _, state := range states {
			lastStateTimestamp = state.StoredAt
			coin, err := z.makeCoin(state.DataJson)
			if err != nil {
				return nil, nil, nil, nil, fmt.Errorf("coin %s is invalid: %s", state.Id, err)
			}
			total = total.Add(total, coin.Amount.Int())
			stateRefs = append(stateRefs, &pb.StateRef{
				SchemaId: state.SchemaId,
				Id:       state.Id,
			})
			coins = append(coins, coin)
			if total.Cmp(expectedTotal) >= 0 {
				remainder := total.Sub(total, expectedTotal)
				return coins, stateRefs, total, remainder, nil
			}
			if len(stateRefs) >= MAX_INPUT_COUNT {
				return nil, nil, nil, nil, fmt.Errorf("could not find enough coins to fulfill the transfer amount total")
			}
		}
	}
}

func (z *Zeto) prepareOutputs(params []*types.TransferParamEntry, resolvedVerifiers []*pb.ResolvedVerifier) ([]*types.ZetoCoin, []*pb.NewState, error) {
	var coins []*types.ZetoCoin
	var newStates []*pb.NewState
	for _, param := range params {
		resolvedRecipient := domain.FindVerifier(param.To, z.getAlgoZetoSnarkBJJ(), zetosignerapi.IDEN3_PUBKEY_BABYJUBJUB_COMPRESSED_0X, resolvedVerifiers)
		if resolvedRecipient == nil {
			return nil, nil, fmt.Errorf("failed to resolve: %s", param.To)
		}
		recipientKey, err := loadBabyJubKey([]byte(resolvedRecipient.Verifier))
		if err != nil {
			return nil, nil, fmt.Errorf("failed load receiver public key. %s", err)
		}

		salt := crypto.NewSalt()
		compressedKeyStr := zetosigner.EncodeBabyJubJubPublicKey(recipientKey)
		newCoin := &types.ZetoCoin{
			Salt:     (*tktypes.HexUint256)(salt),
			Owner:    param.To,
			OwnerKey: tktypes.MustParseHexBytes(compressedKeyStr),
			Amount:   param.Amount,
		}

		newState, err := z.makeNewState(newCoin)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create new state. %s", err)
		}
		coins = append(coins, newCoin)
		newStates = append(newStates, newState)
	}
	return coins, newStates, nil
}

func (z *Zeto) findAvailableStates(ctx context.Context, stateQueryContext, query string) ([]*pb.StoredState, error) {
	req := &pb.FindAvailableStatesRequest{
		StateQueryContext: stateQueryContext,
		SchemaId:          z.coinSchema.Id,
		QueryJson:         query,
	}
	res, err := z.Callbacks.FindAvailableStates(ctx, req)
	if err != nil {
		return nil, err
	}
	return res.States, nil
}
