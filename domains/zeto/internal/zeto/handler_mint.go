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

	"github.com/kaleido-io/paladin/domains/zeto/pkg/types"
	"github.com/kaleido-io/paladin/domains/zeto/pkg/zetosigner/zetosignerapi"
	"github.com/kaleido-io/paladin/toolkit/pkg/algorithms"
	pb "github.com/kaleido-io/paladin/toolkit/pkg/prototk"
	"github.com/kaleido-io/paladin/toolkit/pkg/verifiers"
)

type mintHandler struct {
	zeto *Zeto
}

func (h *mintHandler) ValidateParams(ctx context.Context, config *types.DomainInstanceConfig, params string) (interface{}, error) {
	var mintParams types.MintParams
	if err := json.Unmarshal([]byte(params), &mintParams); err != nil {
		return nil, err
	}

	if err := validateTransferParams(mintParams.Mints); err != nil {
		return nil, err
	}

	return mintParams.Mints, nil
}

func (h *mintHandler) Init(ctx context.Context, tx *types.ParsedTransaction, req *pb.InitTransactionRequest) (*pb.InitTransactionResponse, error) {
	params := tx.Params.([]*types.TransferParamEntry)

	res := &pb.InitTransactionResponse{
		RequiredVerifiers: []*pb.ResolveVerifierRequest{},
	}
	for _, param := range params {
		verifier := &pb.ResolveVerifierRequest{
			Lookup:       param.To,
			Algorithm:    h.zeto.getAlgoZetoSnarkBJJ(),
			VerifierType: zetosignerapi.IDEN3_PUBKEY_BABYJUBJUB_COMPRESSED_0X,
		}
		res.RequiredVerifiers = append(res.RequiredVerifiers, verifier)
	}

	return res, nil
}

func (h *mintHandler) Assemble(ctx context.Context, tx *types.ParsedTransaction, req *pb.AssembleTransactionRequest) (*pb.AssembleTransactionResponse, error) {
	params := tx.Params.([]*types.TransferParamEntry)

	_, outputStates, err := h.zeto.prepareOutputs(params, req.ResolvedVerifiers)
	if err != nil {
		return nil, err
	}
	return &pb.AssembleTransactionResponse{
		AssemblyResult: pb.AssembleTransactionResponse_OK,
		AssembledTransaction: &pb.AssembledTransaction{
			OutputStates: outputStates,
		},
		AttestationPlan: []*pb.AttestationRequest{
			{
				Name:            "submitter",
				AttestationType: pb.AttestationType_ENDORSE,
				Algorithm:       algorithms.ECDSA_SECP256K1,
				VerifierType:    verifiers.ETH_ADDRESS,
				Parties:         []string{tx.Transaction.From},
			},
		},
	}, nil
}

func (h *mintHandler) Endorse(ctx context.Context, tx *types.ParsedTransaction, req *pb.EndorseTransactionRequest) (*pb.EndorseTransactionResponse, error) {
	return &pb.EndorseTransactionResponse{
		EndorsementResult: pb.EndorseTransactionResponse_ENDORSER_SUBMIT,
	}, nil
}

func (h *mintHandler) Prepare(ctx context.Context, tx *types.ParsedTransaction, req *pb.PrepareTransactionRequest) (*pb.PrepareTransactionResponse, error) {
	outputs := make([]string, len(req.OutputStates))
	for i, state := range req.OutputStates {
		coin, err := h.zeto.makeCoin(state.StateDataJson)
		if err != nil {
			return nil, err
		}
		hash, err := coin.Hash()
		if err != nil {
			return nil, err
		}
		outputs[i] = hash.String()
	}

	data, err := encodeTransactionData(ctx, req.Transaction)
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction data. %s", err)
	}
	params := map[string]interface{}{
		"utxos": outputs,
		"data":  data,
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	contractAbi, err := h.zeto.config.GetContractAbi(tx.DomainConfig.TokenName)
	if err != nil {
		return nil, err
	}
	functionJSON, err := json.Marshal(contractAbi.Functions()["mint"])
	if err != nil {
		return nil, err
	}

	return &pb.PrepareTransactionResponse{
		Transaction: &pb.PreparedTransaction{
			FunctionAbiJson: string(functionJSON),
			ParamsJson:      string(paramsJSON),
		},
	}, nil
}
