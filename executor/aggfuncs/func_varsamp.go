MilevaDB Copyright (c) 2022 MilevaDB Authors: Karl Whitford, Spencer Fogelman, Josh Leder
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a INTERLOCKy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package aggfuncs

import (
	"github.com/whtcorpsinc/MilevaDB-Prod/soliton/chunk"
	"github.com/whtcorpsinc/MilevaDB-Prod/stochastikctx"
)

type varSamp4Float64 struct {
	varPop4Float64
}

func (e *varSamp4Float64) AppendFinalResult2Chunk(sctx stochastikctx.Context, pr PartialResult, chk *chunk.Chunk) error {
	p := (*partialResult4VarPopFloat64)(pr)
	if p.count <= 1 {
		chk.AppendNull(e.ordinal)
		return nil
	}
	variance := p.variance / float64(p.count-1)
	chk.AppendFloat64(e.ordinal, variance)
	return nil
}

type varSamp4DistinctFloat64 struct {
	varPop4DistinctFloat64
}

func (e *varSamp4DistinctFloat64) AppendFinalResult2Chunk(sctx stochastikctx.Context, pr PartialResult, chk *chunk.Chunk) error {
	p := (*partialResult4VarPoFIDelistinctFloat64)(pr)
	if p.count <= 1 {
		chk.AppendNull(e.ordinal)
		return nil
	}
	variance := p.variance / float64(p.count-1)
	chk.AppendFloat64(e.ordinal, variance)
	return nil
}