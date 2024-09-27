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

package smt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPackageInit(t *testing.T) {
	assert.Equal(t, SMT_HEIGHT_UTXO, len(Empty_Proof.Nodes))
	assert.Equal(t, "0", Empty_Proof.Nodes[0])
	assert.Equal(t, "0", Empty_Proof.Nodes[SMT_HEIGHT_UTXO-1])
}
