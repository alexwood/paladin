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

	"github.com/kaleido-io/paladin/kata/internal/types"
)

type TimestampField string

func (sf TimestampField) SQLColumn() string {
	return (string)(sf)
}

func (sf TimestampField) SQLValue(ctx context.Context, jsonValue types.RawJSON) (driver.Value, error) {
	var timestamp types.Timestamp
	err := json.Unmarshal(jsonValue, &timestamp)
	return timestamp, err
}
