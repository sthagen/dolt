// Copyright 2019 Liquidata, Inc.
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
//
// This file incorporates work covered by the following copyright and
// permission notice:
//
// Copyright 2016 Attic Labs, Inc. All rights reserved.
// Licensed under the Apache License, version 2.0:
// http://www.apache.org/licenses/LICENSE-2.0

package types

import (
	"context"
	"sync/atomic"

	"github.com/liquidata-inc/ld/dolt/go/store/d"
)

var EmptyList List

// List represents a list or an array of Noms values. A list can contain zero or more values of zero
// or more types. The type of the list will reflect the type of the elements in the list. For
// example:
//
//  l := NewList(Float(1), Bool(true))
//  fmt.Println(l.Type().Describe())
//  // outputs List<Bool | Float>
//
// Lists, like all Noms values are immutable so the "mutation" methods return a new list.
type List struct {
	sequence
}

func newList(seq sequence) List {
	return List{seq}
}

// NewList creates a new List where the type is computed from the elements in the list, populated
// with values, chunking if and when needed.
func NewList(ctx context.Context, vrw ValueReadWriter, values ...Value) List {
	ch := newEmptyListSequenceChunker(ctx, vrw)
	for _, v := range values {
		ch.Append(ctx, v)
	}
	return newList(ch.Done(ctx))
}

// NewStreamingList creates a new List, populated with values, chunking if and when needed. As
// chunks are created, they're written to vrw -- including the root chunk of the list. Once the
// caller has closed values, the caller can read the completed List from the returned channel.
func NewStreamingList(ctx context.Context, vrw ValueReadWriter, values <-chan Value) <-chan List {
	out := make(chan List, 1)
	go func() {
		defer close(out)
		ch := newEmptyListSequenceChunker(ctx, vrw)
		for v := range values {
			ch.Append(ctx, v)
		}
		out <- newList(ch.Done(ctx))
	}()
	return out
}

func (l List) Edit() *ListEditor {
	return NewListEditor(l)
}

// Collection interface

func (l List) asSequence() sequence {
	return l.sequence
}

// Value interface
func (l List) Value(ctx context.Context) Value {
	return l
}

func (l List) WalkValues(ctx context.Context, cb ValueCallback) {
	iterAll(ctx, l, func(v Value, idx uint64) {
		cb(v)
	})
}

// Get returns the value at the given index. If this list has been chunked then this will have to
// descend into the prolly-tree which leads to Get being O(depth).
func (l List) Get(ctx context.Context, idx uint64) Value {
	d.PanicIfFalse(idx < l.Len())
	cur := newCursorAtIndex(ctx, l.sequence, idx)
	return cur.current().(Value)
}

// Concat returns a new List comprised of this joined with other. It only needs
// to visit the rightmost prolly tree chunks of this List, and the leftmost
// prolly tree chunks of other, so it's efficient.
func (l List) Concat(ctx context.Context, other List) List {
	seq := concat(ctx, l.sequence, other.sequence, func(cur *sequenceCursor, vrw ValueReadWriter) *sequenceChunker {
		return l.newChunker(ctx, cur, vrw)
	})
	return newList(seq)
}

// Iter iterates over the list and calls f for every element in the list. If f returns true then the
// iteration stops.
func (l List) Iter(ctx context.Context, f func(v Value, index uint64) (stop bool)) {
	idx := uint64(0)
	cur := newCursorAtIndex(ctx, l.sequence, idx)
	cur.iter(ctx, func(v interface{}) bool {
		if f(v.(Value), uint64(idx)) {
			return true
		}
		idx++
		return false
	})
}

func (l List) IterRange(ctx context.Context, startIdx, endIdx uint64, f func(v Value, idx uint64)) {
	idx := uint64(startIdx)
	cb := func(v Value) {
		f(v, idx)
		idx++
	}
	iterRange(ctx, l, startIdx, endIdx, cb)
}

// IterAll iterates over the list and calls f for every element in the list. Unlike Iter there is no
// way to stop the iteration and all elements are visited.
func (l List) IterAll(ctx context.Context, f func(v Value, index uint64)) {
	iterAll(ctx, l, f)
}

func iterAll(ctx context.Context, col Collection, f func(v Value, index uint64)) {
	concurrency := 6
	vcChan := make(chan chan Value, concurrency)

	// Target reading data in |targetBatchBytes| per thread. We don't know how
	// many bytes each value is, so update |estimatedNumValues| as data is read.
	targetBatchBytes := 1 << 23 // 8MB
	estimatedNumValues := uint64(1000)

	go func() {
		for idx, l := uint64(0), col.Len(); idx < l; {
			numValues := atomic.LoadUint64(&estimatedNumValues)

			start := idx
			blockLength := l - start
			if blockLength > numValues {
				blockLength = numValues
			}
			idx += blockLength

			vc := make(chan Value)
			vcChan <- vc

			go func() {
				numBytes := iterRange(ctx, col, start, start+blockLength, func(v Value) {
					vc <- v
				})
				close(vc)

				// Adjust the estimated number of values to try to read
				// |targetBatchBytes| next time.
				if numValues == blockLength {
					scale := float64(targetBatchBytes) / float64(numBytes)
					atomic.StoreUint64(&estimatedNumValues, uint64(float64(numValues)*scale))
				}
			}()
		}
		close(vcChan)
	}()

	// Ensure read-ahead goroutines can exit, because the `range` below might not
	// finish if an |f| callback panics.
	defer func() {
		for vc := range vcChan {
			close(vc)
		}
	}()

	i := uint64(0)
	for vc := range vcChan {
		for v := range vc {
			f(v, i)
			i++
		}
	}
}

func iterRange(ctx context.Context, col Collection, startIdx, endIdx uint64, cb func(v Value)) (numBytes uint64) {
	l := col.Len()
	d.PanicIfTrue(startIdx > endIdx || endIdx > l)
	if startIdx == endIdx {
		return
	}

	leaves, localStart := LoadLeafNodes(ctx, []Collection{col}, startIdx, endIdx)
	endIdx = localStart + endIdx - startIdx
	startIdx = localStart
	numValues := 0
	valuesPerIdx := uint64(getValuesPerIdx(col.Kind()))

	for _, leaf := range leaves {
		seq := leaf.asSequence()
		values := seq.valuesSlice(startIdx, endIdx)
		numValues += len(values)

		for _, v := range values {
			cb(v)
		}

		endIdx = endIdx - uint64(len(values))/valuesPerIdx - startIdx
		startIdx = 0
		numBytes += uint64(len(seq.valueBytes(seq.format()))) // note: should really only include |values|
	}
	return
}

// Iterator returns a ListIterator which can be used to iterate efficiently over a list.
func (l List) Iterator(ctx context.Context) ListIterator {
	return l.IteratorAt(ctx, 0)
}

// IteratorAt returns a ListIterator starting at index. If index is out of bound the iterator will
// have reached its end on creation.
func (l List) IteratorAt(ctx context.Context, index uint64) ListIterator {
	return ListIterator{
		newCursorAtIndex(ctx, l.sequence, index),
	}
}

func (l List) Format() *NomsBinFormat {
	return l.format()
}

// Diff streams the diff from last to the current list to the changes channel. Caller can close
// closeChan to cancel the diff operation.
func (l List) Diff(ctx context.Context, last List, changes chan<- Splice, closeChan <-chan struct{}) {
	l.DiffWithLimit(ctx, last, changes, closeChan, DEFAULT_MAX_SPLICE_MATRIX_SIZE)
}

// DiffWithLimit streams the diff from last to the current list to the changes channel. Caller can
// close closeChan to cancel the diff operation.
// The maxSpliceMatrixSize determines the how big of an edit distance matrix we are willing to
// compute versus just saying the thing changed.
func (l List) DiffWithLimit(ctx context.Context, last List, changes chan<- Splice, closeChan <-chan struct{}, maxSpliceMatrixSize uint64) {
	if l.Equals(last) {
		return
	}
	lLen, lastLen := l.Len(), last.Len()
	if lLen == 0 {
		changes <- Splice{0, lastLen, 0, 0} // everything removed
		return
	}
	if lastLen == 0 {
		changes <- Splice{0, 0, lLen, 0} // everything added
		return
	}

	indexedSequenceDiff(ctx, last.sequence, 0, l.sequence, 0, changes, closeChan, maxSpliceMatrixSize)
}

func (l List) newChunker(ctx context.Context, cur *sequenceCursor, vrw ValueReadWriter) *sequenceChunker {
	return newSequenceChunker(ctx, cur, 0, vrw, makeListLeafChunkFn(vrw), newIndexedMetaSequenceChunkFn(ListKind, vrw), hashValueBytes)
}

func makeListLeafChunkFn(vrw ValueReadWriter) makeChunkFn {
	return func(level uint64, items []sequenceItem) (Collection, orderedKey, uint64) {
		d.PanicIfFalse(level == 0)
		values := make([]Value, len(items))

		for i, v := range items {
			values[i] = v.(Value)
		}

		list := newList(newListLeafSequence(vrw, values...))
		return list, orderedKeyFromInt(len(values), vrw.Format()), uint64(len(values))
	}
}

func newEmptyListSequenceChunker(ctx context.Context, vrw ValueReadWriter) *sequenceChunker {
	return newEmptySequenceChunker(ctx, vrw, makeListLeafChunkFn(vrw), newIndexedMetaSequenceChunkFn(ListKind, vrw), hashValueBytes)
}