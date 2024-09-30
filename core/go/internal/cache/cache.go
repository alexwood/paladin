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

package cache

import (
	"github.com/Code-Hex/go-generics-cache/policy/lru"
	"github.com/kaleido-io/paladin/toolkit/pkg/confutil"
)

type Config struct {
	Capacity *int `json:"capacity"`
}

type Cache[K comparable, V any] interface {
	Get(key K) (V, bool)
	Set(key K, val V)
	Delete(key K)
	Capacity() int
}

type cache[K comparable, V any] struct {
	cache    *lru.Cache[K, V]
	capacity int
}

func NewCache[K comparable, V any](conf *Config, defs *Config) Cache[K, V] {
	capacity := confutil.Int(conf.Capacity, *defs.Capacity)
	c := &cache[K, V]{
		capacity: capacity,
		cache: lru.NewCache[K, V](
			lru.WithCapacity(capacity),
		),
	}
	return c
}

func (c *cache[K, V]) Get(key K) (V, bool) {
	return c.cache.Get(key)
}

func (c *cache[K, V]) Set(key K, val V) {
	c.cache.Set(key, val)
}

func (c *cache[K, V]) Delete(key K) {
	c.cache.Delete(key)
}

func (c *cache[K, V]) Capacity() int {
	return c.capacity
}
