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

package filters

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strings"

	"github.com/kaleido-io/paladin/common/go/pkg/i18n"
	"github.com/kaleido-io/paladin/core/internal/msgs"
	"github.com/kaleido-io/paladin/sdk/go/pkg/pldtypes"
)

type BooleanField string

func (sf BooleanField) SQLColumn() string {
	return (string)(sf)
}

func (sf BooleanField) SupportsLIKE() bool {
	return false
}

func (sf BooleanField) SQLValue(ctx context.Context, jsonValue pldtypes.RawJSON) (driver.Value, error) {
	if jsonValue.IsNil() {
		return nil, nil
	}
	var untyped interface{}
	err := json.Unmarshal(jsonValue, &untyped)
	if err != nil {
		return nil, err
	}
	switch v := untyped.(type) {
	case string:
		if strings.EqualFold(v, "true") {
			return true, nil
		}
		return false, nil
	case bool:
		return v, nil
	default:
		return nil, i18n.NewError(ctx, msgs.MsgFiltersValueInvalidForBool, string(jsonValue))
	}
}
