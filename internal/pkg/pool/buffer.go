/*
Copyright 2024 NovaEdge Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package pool provides sync.Pool implementations for commonly allocated objects
// to reduce GC pressure in hot paths.
package pool

import (
	"bytes"
	"strings"
	"sync"
)

// BufferPool is a sync.Pool for bytes.Buffer objects.
// Use this for hot paths that frequently allocate byte buffers.
var BufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// GetBuffer retrieves a bytes.Buffer from the pool.
// The buffer is reset before being returned.
// Call PutBuffer after use to return it to the pool.
func GetBuffer() *bytes.Buffer {
	buf, ok := BufferPool.Get().(*bytes.Buffer)
	if !ok {
		return new(bytes.Buffer)
	}
	buf.Reset()
	return buf
}

// PutBuffer returns a bytes.Buffer to the pool.
// Do not use the buffer after calling this function.
func PutBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// Reset to allow GC on the underlying slice
	buf.Reset()
	BufferPool.Put(buf)
}

// StringBuilderPool is a sync.Pool for strings.Builder objects.
// Use this for hot paths that frequently build strings.
var StringBuilderPool = sync.Pool{
	New: func() interface{} {
		return new(strings.Builder)
	},
}

// GetStringBuilder retrieves a strings.Builder from the pool.
// The builder is reset before being returned.
// Call PutStringBuilder after use to return it to the pool.
func GetStringBuilder() *strings.Builder {
	sb, ok := StringBuilderPool.Get().(*strings.Builder)
	if !ok {
		return new(strings.Builder)
	}
	sb.Reset()
	return sb
}

// PutStringBuilder returns a strings.Builder to the pool.
// Do not use the builder after calling this function.
func PutStringBuilder(sb *strings.Builder) {
	if sb == nil {
		return
	}
	sb.Reset()
	StringBuilderPool.Put(sb)
}

// ByteSlicePool is a sync.Pool for reusable byte slices.
// This is useful for operations that need temporary byte buffers.
type ByteSlicePool struct {
	pool sync.Pool
	size int
}

// NewByteSlicePool creates a new pool for byte slices of the given size.
func NewByteSlicePool(size int) *ByteSlicePool {
	return &ByteSlicePool{
		size: size,
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, size)
				return &buf
			},
		},
	}
}

// Get retrieves a byte slice from the pool.
// The slice is guaranteed to be of the configured size.
func (p *ByteSlicePool) Get() []byte {
	buf, ok := p.pool.Get().(*[]byte)
	if !ok {
		b := make([]byte, p.size)
		return b
	}
	return *buf
}

// Put returns a byte slice to the pool.
// Do not use the slice after calling this function.
func (p *ByteSlicePool) Put(buf []byte) {
	if cap(buf) < p.size {
		return // Don't pool undersized buffers
	}
	p.pool.Put(&buf)
}

// SmallByteSlicePool is a pool for 4KB byte slices (common page size).
var SmallByteSlicePool = NewByteSlicePool(4096)

// MediumByteSlicePool is a pool for 32KB byte slices.
var MediumByteSlicePool = NewByteSlicePool(32 * 1024)

// LargeByteSlicePool is a pool for 128KB byte slices.
var LargeByteSlicePool = NewByteSlicePool(128 * 1024)
