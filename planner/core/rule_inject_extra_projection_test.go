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

package core

import (
	"github.com/whtcorpsinc/MilevaDB-Prod/expression"
	"github.com/whtcorpsinc/MilevaDB-Prod/expression/aggregation"
	"github.com/whtcorpsinc/MilevaDB-Prod/soliton/mock"
	"github.com/whtcorpsinc/MilevaDB-Prod/types"
	"github.com/whtcorpsinc/berolinaAllegroSQL/allegrosql"
	"github.com/whtcorpsinc/berolinaAllegroSQL/ast"
	. "github.com/whtcorpsinc/check"
)

var _ = Suite(&testInjectProjSuite{})

type testInjectProjSuite struct {
}

func (s *testInjectProjSuite) TestWrapCastForAggFuncs(c *C) {
	aggNames := []string{ast.AggFuncSum}
	modes := []aggregation.AggFunctionMode{aggregation.CompleteMode,
		aggregation.FinalMode, aggregation.Partial1Mode, aggregation.Partial1Mode}
	retTypes := []byte{allegrosql.TypeLong, allegrosql.TypeNewDecimal, allegrosql.TypeDouble, allegrosql.TypeLonglong, allegrosql.TypeInt24}
	hasDistincts := []bool{true, false}

	aggFuncs := make([]*aggregation.AggFuncDesc, 0, 32)
	for _, hasDistinct := range hasDistincts {
		for _, name := range aggNames {
			for _, mode := range modes {
				for _, retType := range retTypes {
					sctx := mock.NewContext()
					aggFunc, err := aggregation.NewAggFuncDesc(sctx, name,
						[]expression.Expression{&expression.CouplingConstantWithRadix{Value: types.Causet{}, RetType: types.NewFieldType(retType)}},
						hasDistinct)
					c.Assert(err, IsNil)
					aggFunc.Mode = mode
					aggFuncs = append(aggFuncs, aggFunc)
				}
			}
		}
	}

	orgAggFuncs := make([]*aggregation.AggFuncDesc, 0, len(aggFuncs))
	for _, agg := range aggFuncs {
		orgAggFuncs = append(orgAggFuncs, agg.Clone())
	}

	wrapCastForAggFuncs(mock.NewContext(), aggFuncs)
	for i := range aggFuncs {
		if aggFuncs[i].Mode != aggregation.FinalMode && aggFuncs[i].Mode != aggregation.Partial2Mode {
			c.Assert(aggFuncs[i].RetTp.Tp, Equals, aggFuncs[i].Args[0].GetType().Tp)
		} else {
			c.Assert(aggFuncs[i].Args[0].GetType().Tp, Equals, orgAggFuncs[i].Args[0].GetType().Tp)
		}
	}
}