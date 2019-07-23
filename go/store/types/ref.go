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
	"bytes"
	"context"

	"github.com/liquidata-inc/ld/dolt/go/store/hash"
)

type Ref struct {
	valueImpl
}

type refPart uint32

const (
	refPartKind refPart = iota
	refPartTargetHash
	refPartTargetType
	refPartHeight
	refPartEnd
)

func NewRef(v Value, nbf *NomsBinFormat) Ref {
	return constructRef(nbf, v.Hash(nbf), TypeOf(v), maxChunkHeight(nbf, v)+1)
}

// ToRefOfValue returns a new Ref that points to the same target as |r|, but
// with the type 'Ref<Value>'.
func ToRefOfValue(r Ref, nbf *NomsBinFormat) Ref {
	return constructRef(nbf, r.TargetHash(), ValueType, r.Height())
}

func constructRef(nbf *NomsBinFormat, targetHash hash.Hash, targetType *Type, height uint64) Ref {
	w := newBinaryNomsWriter()

	offsets := make([]uint32, refPartEnd)
	offsets[refPartKind] = w.offset
	RefKind.writeTo(&w, nbf)
	offsets[refPartTargetHash] = w.offset
	w.writeHash(targetHash)
	offsets[refPartTargetType] = w.offset
	targetType.writeToAsType(&w, map[string]*Type{}, nbf)
	offsets[refPartHeight] = w.offset
	w.writeCount(height)

	return Ref{valueImpl{nil, nbf, w.data(), offsets}}
}

// readRef reads the data provided by a reader and moves the reader forward.
func readRef(nbf *NomsBinFormat, dec *typedBinaryNomsReader) Ref {
	start := dec.pos()
	offsets := skipRef(dec)
	end := dec.pos()
	return Ref{valueImpl{nil, nbf, dec.byteSlice(start, end), offsets}}
}

// skipRef moves the reader forward, past the data representing the Ref, and returns the offsets of the component parts.
func skipRef(dec *typedBinaryNomsReader) []uint32 {
	offsets := make([]uint32, refPartEnd)
	offsets[refPartKind] = dec.pos()
	dec.skipKind()
	offsets[refPartTargetHash] = dec.pos()
	dec.skipHash() // targetHash
	offsets[refPartTargetType] = dec.pos()
	dec.skipType() // targetType
	offsets[refPartHeight] = dec.pos()
	dec.skipCount() // height
	return offsets
}

func maxChunkHeight(nbf *NomsBinFormat, v Value) (max uint64) {
	v.WalkRefs(nbf, func(r Ref) {
		if height := r.Height(); height > max {
			max = height
		}
	})
	return
}

func (r Ref) offsetAtPart(part refPart) uint32 {
	return r.offsets[part] - r.offsets[refPartKind]
}

func (r Ref) decoderAtPart(part refPart) valueDecoder {
	offset := r.offsetAtPart(part)
	return newValueDecoder(r.buff[offset:], nil)
}

func (r Ref) Format() *NomsBinFormat {
	return r.format()
}

func (r Ref) TargetHash() hash.Hash {
	dec := r.decoderAtPart(refPartTargetHash)
	return dec.readHash()
}

func (r Ref) Height() uint64 {
	dec := r.decoderAtPart(refPartHeight)
	return dec.readCount()
}

func (r Ref) TargetValue(ctx context.Context, vr ValueReader) Value {
	return vr.ReadValue(ctx, r.TargetHash())
}

func (r Ref) TargetType() *Type {
	dec := r.decoderAtPart(refPartTargetType)
	return dec.readType()
}

// Value interface
func (r Ref) Value(ctx context.Context) Value {
	return r
}

func (r Ref) WalkValues(ctx context.Context, cb ValueCallback) {
}

func (r Ref) typeOf() *Type {
	return makeCompoundType(RefKind, r.TargetType())
}

func (r Ref) isSameTargetType(other Ref) bool {
	targetTypeBytes := r.buff[r.offsetAtPart(refPartTargetType):r.offsetAtPart(refPartHeight)]
	otherTargetTypeBytes := other.buff[other.offsetAtPart(refPartTargetType):other.offsetAtPart(refPartHeight)]
	return bytes.Equal(targetTypeBytes, otherTargetTypeBytes)
}