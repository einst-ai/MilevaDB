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

package expression

import (
	"fmt"
	"math"

	"github.com/cznic/mathutil"
	"github.com/whtcorpsinc/berolinaAllegroSQL/allegrosql"
	"github.com/whtcorpsinc/berolinaAllegroSQL/terror"
	"github.com/whtcorpsinc/fidelpb/go-fidelpb"
	"github.com/whtcorpsinc/MilevaDB-Prod/soliton/chunk"
	"github.com/whtcorpsinc/MilevaDB-Prod/stochastikctx"
	"github.com/whtcorpsinc/MilevaDB-Prod/types"
)

var (
	_ functionClass = &arithmeticPlusFunctionClass{}
	_ functionClass = &arithmeticMinusFunctionClass{}
	_ functionClass = &arithmeticDivideFunctionClass{}
	_ functionClass = &arithmeticMultiplyFunctionClass{}
	_ functionClass = &arithmeticIntDivideFunctionClass{}
	_ functionClass = &arithmeticModFunctionClass{}
)

var (
	_ builtinFunc = &builtinArithmeticPlusRealSig{}
	_ builtinFunc = &builtinArithmeticPlusDecimalSig{}
	_ builtinFunc = &builtinArithmeticPlusIntSig{}
	_ builtinFunc = &builtinArithmeticMinusRealSig{}
	_ builtinFunc = &builtinArithmeticMinusDecimalSig{}
	_ builtinFunc = &builtinArithmeticMinusIntSig{}
	_ builtinFunc = &builtinArithmeticDivideRealSig{}
	_ builtinFunc = &builtinArithmeticDivideDecimalSig{}
	_ builtinFunc = &builtinArithmeticMultiplyRealSig{}
	_ builtinFunc = &builtinArithmeticMultiplyDecimalSig{}
	_ builtinFunc = &builtinArithmeticMultiplyIntUnsignedSig{}
	_ builtinFunc = &builtinArithmeticMultiplyIntSig{}
	_ builtinFunc = &builtinArithmeticIntDivideIntSig{}
	_ builtinFunc = &builtinArithmeticIntDivideDecimalSig{}
	_ builtinFunc = &builtinArithmeticModIntSig{}
	_ builtinFunc = &builtinArithmeticModRealSig{}
	_ builtinFunc = &builtinArithmeticModDecimalSig{}
)

// precIncrement indicates the number of digits by which to increase the scale of the result of division operations
// performed with the / operator.
const precIncrement = 4

// numericContextResultType returns types.EvalType for numeric function's parameters.
// the returned types.EvalType should be one of: types.CausetEDN, types.ETDecimal, types.ETReal
func numericContextResultType(ft *types.FieldType) types.EvalType {
	if types.IsTypeTemporal(ft.Tp) {
		if ft.Decimal > 0 {
			return types.ETDecimal
		}
		return types.CausetEDN
	}
	if types.IsBinaryStr(ft) {
		return types.CausetEDN
	}
	evalTp4Ft := types.ETReal
	if !ft.Hybrid() {
		evalTp4Ft = ft.EvalType()
		if evalTp4Ft != types.ETDecimal && evalTp4Ft != types.CausetEDN {
			evalTp4Ft = types.ETReal
		}
	}
	return evalTp4Ft
}

// setFlenDecimal4Int is called to set proper `Flen` and `Decimal` of return
// type according to the two input parameter's types.
func setFlenDecimal4Int(retTp, a, b *types.FieldType) {
	retTp.Decimal = 0
	retTp.Flen = allegrosql.MaxIntWidth
}

// setFlenDecimal4RealOrDecimal is called to set proper `Flen` and `Decimal` of return
// type according to the two input parameter's types.
func setFlenDecimal4RealOrDecimal(retTp, a, b *types.FieldType, isReal bool, isMultiply bool) {
	if a.Decimal != types.UnspecifiedLength && b.Decimal != types.UnspecifiedLength {
		retTp.Decimal = a.Decimal + b.Decimal
		if !isMultiply {
			retTp.Decimal = mathutil.Max(a.Decimal, b.Decimal)
		}
		if !isReal && retTp.Decimal > allegrosql.MaxDecimalScale {
			retTp.Decimal = allegrosql.MaxDecimalScale
		}
		if a.Flen == types.UnspecifiedLength || b.Flen == types.UnspecifiedLength {
			retTp.Flen = types.UnspecifiedLength
			return
		}
		digitsInt := mathutil.Max(a.Flen-a.Decimal, b.Flen-b.Decimal)
		if isMultiply {
			digitsInt = a.Flen - a.Decimal + b.Flen - b.Decimal
		}
		retTp.Flen = digitsInt + retTp.Decimal + 3
		if isReal {
			retTp.Flen = mathutil.Min(retTp.Flen, allegrosql.MaxRealWidth)
			return
		}
		retTp.Flen = mathutil.Min(retTp.Flen, allegrosql.MaxDecimalWidth)
		return
	}
	if isReal {
		retTp.Flen, retTp.Decimal = types.UnspecifiedLength, types.UnspecifiedLength
	} else {
		retTp.Flen, retTp.Decimal = allegrosql.MaxDecimalWidth, allegrosql.MaxDecimalScale
	}
}

func (c *arithmeticDivideFunctionClass) setType4DivDecimal(retTp, a, b *types.FieldType) {
	var deca, decb = a.Decimal, b.Decimal
	if deca == int(types.UnspecifiedFsp) {
		deca = 0
	}
	if decb == int(types.UnspecifiedFsp) {
		decb = 0
	}
	retTp.Decimal = deca + precIncrement
	if retTp.Decimal > allegrosql.MaxDecimalScale {
		retTp.Decimal = allegrosql.MaxDecimalScale
	}
	if a.Flen == types.UnspecifiedLength {
		retTp.Flen = allegrosql.MaxDecimalWidth
		return
	}
	retTp.Flen = a.Flen + decb + precIncrement
	if retTp.Flen > allegrosql.MaxDecimalWidth {
		retTp.Flen = allegrosql.MaxDecimalWidth
	}
}

func (c *arithmeticDivideFunctionClass) setType4DivReal(retTp *types.FieldType) {
	retTp.Decimal = types.UnspecifiedLength
	retTp.Flen = allegrosql.MaxRealWidth
}

type arithmeticPlusFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticPlusFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}
	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.ETReal || rhsEvalTp == types.ETReal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETReal, types.ETReal, types.ETReal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), true, false)
		sig := &builtinArithmeticPlusRealSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_PlusReal)
		return sig, nil
	} else if lhsEvalTp == types.ETDecimal || rhsEvalTp == types.ETDecimal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETDecimal, types.ETDecimal, types.ETDecimal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), false, false)
		sig := &builtinArithmeticPlusDecimalSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_PlusDecimal)
		return sig, nil
	} else {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.CausetEDN, types.CausetEDN)
		if err != nil {
			return nil, err
		}
		if allegrosql.HasUnsignedFlag(args[0].GetType().Flag) || allegrosql.HasUnsignedFlag(args[1].GetType().Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		setFlenDecimal4Int(bf.tp, args[0].GetType(), args[1].GetType())
		sig := &builtinArithmeticPlusIntSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_PlusInt)
		return sig, nil
	}
}

type builtinArithmeticPlusIntSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticPlusIntSig) Clone() builtinFunc {
	newSig := &builtinArithmeticPlusIntSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticPlusIntSig) evalInt(event chunk.Event) (val int64, isNull bool, err error) {
	a, isNull, err := s.args[0].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	b, isNull, err := s.args[1].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	isLHSUnsigned := allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag)
	isRHSUnsigned := allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag)

	switch {
	case isLHSUnsigned && isRHSUnsigned:
		if uint64(a) > math.MaxUint64-uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
	case isLHSUnsigned && !isRHSUnsigned:
		if b < 0 && uint64(-b) > uint64(a) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
		if b > 0 && uint64(a) > math.MaxUint64-uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
	case !isLHSUnsigned && isRHSUnsigned:
		if a < 0 && uint64(-a) > uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
		if a > 0 && uint64(b) > math.MaxUint64-uint64(a) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
	case !isLHSUnsigned && !isRHSUnsigned:
		if (a > 0 && b > math.MaxInt64-a) || (a < 0 && b < math.MinInt64-a) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
		}
	}

	return a + b, false, nil
}

type builtinArithmeticPlusDecimalSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticPlusDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticPlusDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticPlusDecimalSig) evalDecimal(event chunk.Event) (*types.MyDecimal, bool, error) {
	a, isNull, err := s.args[0].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	b, isNull, err := s.args[1].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	c := &types.MyDecimal{}
	err = types.DecimalAdd(a, b, c)
	if err != nil {
		return nil, true, err
	}
	return c, false, nil
}

type builtinArithmeticPlusRealSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticPlusRealSig) Clone() builtinFunc {
	newSig := &builtinArithmeticPlusRealSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticPlusRealSig) evalReal(event chunk.Event) (float64, bool, error) {
	a, isLHSNull, err := s.args[0].EvalReal(s.ctx, event)
	if err != nil {
		return 0, isLHSNull, err
	}
	b, isRHSNull, err := s.args[1].EvalReal(s.ctx, event)
	if err != nil {
		return 0, isRHSNull, err
	}
	if isLHSNull || isRHSNull {
		return 0, true, nil
	}
	if (a > 0 && b > math.MaxFloat64-a) || (a < 0 && b < -math.MaxFloat64-a) {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("DOUBLE", fmt.Sprintf("(%s + %s)", s.args[0].String(), s.args[1].String()))
	}
	return a + b, false, nil
}

type arithmeticMinusFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticMinusFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}
	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.ETReal || rhsEvalTp == types.ETReal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETReal, types.ETReal, types.ETReal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), true, false)
		sig := &builtinArithmeticMinusRealSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MinusReal)
		return sig, nil
	} else if lhsEvalTp == types.ETDecimal || rhsEvalTp == types.ETDecimal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETDecimal, types.ETDecimal, types.ETDecimal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), false, false)
		sig := &builtinArithmeticMinusDecimalSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MinusDecimal)
		return sig, nil
	} else {

		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.CausetEDN, types.CausetEDN)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4Int(bf.tp, args[0].GetType(), args[1].GetType())
		if (allegrosql.HasUnsignedFlag(args[0].GetType().Flag) || allegrosql.HasUnsignedFlag(args[1].GetType().Flag)) && !ctx.GetStochaseinstein_dbars().ALLEGROSQLMode.HasNoUnsignedSubtractionMode() {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		sig := &builtinArithmeticMinusIntSig{baseBuiltinFunc: bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MinusInt)
		return sig, nil
	}
}

type builtinArithmeticMinusRealSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticMinusRealSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMinusRealSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticMinusRealSig) evalReal(event chunk.Event) (float64, bool, error) {
	a, isNull, err := s.args[0].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	b, isNull, err := s.args[1].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	if (a > 0 && -b > math.MaxFloat64-a) || (a < 0 && -b < -math.MaxFloat64-a) {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("DOUBLE", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
	}
	return a - b, false, nil
}

type builtinArithmeticMinusDecimalSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticMinusDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMinusDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticMinusDecimalSig) evalDecimal(event chunk.Event) (*types.MyDecimal, bool, error) {
	a, isNull, err := s.args[0].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	b, isNull, err := s.args[1].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	c := &types.MyDecimal{}
	err = types.DecimalSub(a, b, c)
	if err != nil {
		return nil, true, err
	}
	return c, false, nil
}

type builtinArithmeticMinusIntSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticMinusIntSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMinusIntSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticMinusIntSig) evalInt(event chunk.Event) (val int64, isNull bool, err error) {
	a, isNull, err := s.args[0].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	b, isNull, err := s.args[1].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	forceToSigned := s.ctx.GetStochaseinstein_dbars().ALLEGROSQLMode.HasNoUnsignedSubtractionMode()
	isLHSUnsigned := !forceToSigned && allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag)
	isRHSUnsigned := !forceToSigned && allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag)

	if forceToSigned && allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag) {
		if a < 0 {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	}
	if forceToSigned && allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag) {
		if b < 0 {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	}

	switch {
	case isLHSUnsigned && isRHSUnsigned:
		if uint64(a) < uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	case isLHSUnsigned && !isRHSUnsigned:
		if b >= 0 && uint64(a) < uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
		if b < 0 && uint64(a) > math.MaxUint64-uint64(-b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	case !isLHSUnsigned && isRHSUnsigned:
		if a < 0 || uint64(a) < uint64(b) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	case !isLHSUnsigned && !isRHSUnsigned:
		// We need `(a >= 0 && b == math.MinInt64)` due to `-(math.MinInt64) == math.MinInt64`.
		// If `a<0 && b<=0`: `a-b` will not overflow even though b==math.MinInt64.
		// If `a<0 && b>0`: `a-b` will not overflow only if `math.MinInt64<=a-b` satisfied
		if (a >= 0 && b == math.MinInt64) || (a > 0 && -b > math.MaxInt64-a) || (a < 0 && -b < math.MinInt64-a) {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT", fmt.Sprintf("(%s - %s)", s.args[0].String(), s.args[1].String()))
		}
	}
	return a - b, false, nil
}

type arithmeticMultiplyFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticMultiplyFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}
	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.ETReal || rhsEvalTp == types.ETReal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETReal, types.ETReal, types.ETReal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), true, true)
		sig := &builtinArithmeticMultiplyRealSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MultiplyReal)
		return sig, nil
	} else if lhsEvalTp == types.ETDecimal || rhsEvalTp == types.ETDecimal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETDecimal, types.ETDecimal, types.ETDecimal)
		if err != nil {
			return nil, err
		}
		setFlenDecimal4RealOrDecimal(bf.tp, args[0].GetType(), args[1].GetType(), false, true)
		sig := &builtinArithmeticMultiplyDecimalSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MultiplyDecimal)
		return sig, nil
	} else {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.CausetEDN, types.CausetEDN)
		if err != nil {
			return nil, err
		}
		if allegrosql.HasUnsignedFlag(lhsTp.Flag) || allegrosql.HasUnsignedFlag(rhsTp.Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
			setFlenDecimal4Int(bf.tp, args[0].GetType(), args[1].GetType())
			sig := &builtinArithmeticMultiplyIntUnsignedSig{bf}
			sig.setPbCode(fidelpb.ScalarFuncSig_MultiplyIntUnsigned)
			return sig, nil
		}
		setFlenDecimal4Int(bf.tp, args[0].GetType(), args[1].GetType())
		sig := &builtinArithmeticMultiplyIntSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_MultiplyInt)
		return sig, nil
	}
}

type builtinArithmeticMultiplyRealSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticMultiplyRealSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMultiplyRealSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

type builtinArithmeticMultiplyDecimalSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticMultiplyDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMultiplyDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

type builtinArithmeticMultiplyIntUnsignedSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticMultiplyIntUnsignedSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMultiplyIntUnsignedSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

type builtinArithmeticMultiplyIntSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticMultiplyIntSig) Clone() builtinFunc {
	newSig := &builtinArithmeticMultiplyIntSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticMultiplyRealSig) evalReal(event chunk.Event) (float64, bool, error) {
	a, isNull, err := s.args[0].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	b, isNull, err := s.args[1].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	result := a * b
	if math.IsInf(result, 0) {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("DOUBLE", fmt.Sprintf("(%s * %s)", s.args[0].String(), s.args[1].String()))
	}
	return result, false, nil
}

func (s *builtinArithmeticMultiplyDecimalSig) evalDecimal(event chunk.Event) (*types.MyDecimal, bool, error) {
	a, isNull, err := s.args[0].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	b, isNull, err := s.args[1].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	c := &types.MyDecimal{}
	err = types.DecimalMul(a, b, c)
	if err != nil && !terror.ErrorEqual(err, types.ErrTruncated) {
		return nil, true, err
	}
	return c, false, nil
}

func (s *builtinArithmeticMultiplyIntUnsignedSig) evalInt(event chunk.Event) (val int64, isNull bool, err error) {
	a, isNull, err := s.args[0].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	unsignedA := uint64(a)
	b, isNull, err := s.args[1].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	unsignedB := uint64(b)
	result := unsignedA * unsignedB
	if unsignedA != 0 && result/unsignedA != unsignedB {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s * %s)", s.args[0].String(), s.args[1].String()))
	}
	return int64(result), false, nil
}

func (s *builtinArithmeticMultiplyIntSig) evalInt(event chunk.Event) (val int64, isNull bool, err error) {
	a, isNull, err := s.args[0].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	b, isNull, err := s.args[1].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	result := a * b
	if a != 0 && result/a != b {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT", fmt.Sprintf("(%s * %s)", s.args[0].String(), s.args[1].String()))
	}
	return result, false, nil
}

type arithmeticDivideFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticDivideFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}
	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.ETReal || rhsEvalTp == types.ETReal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETReal, types.ETReal, types.ETReal)
		if err != nil {
			return nil, err
		}
		c.setType4DivReal(bf.tp)
		sig := &builtinArithmeticDivideRealSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_DivideReal)
		return sig, nil
	}
	bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETDecimal, types.ETDecimal, types.ETDecimal)
	if err != nil {
		return nil, err
	}
	c.setType4DivDecimal(bf.tp, lhsTp, rhsTp)
	sig := &builtinArithmeticDivideDecimalSig{bf}
	sig.setPbCode(fidelpb.ScalarFuncSig_DivideDecimal)
	return sig, nil
}

type builtinArithmeticDivideRealSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticDivideRealSig) Clone() builtinFunc {
	newSig := &builtinArithmeticDivideRealSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

type builtinArithmeticDivideDecimalSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticDivideDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticDivideDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticDivideRealSig) evalReal(event chunk.Event) (float64, bool, error) {
	a, isNull, err := s.args[0].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	b, isNull, err := s.args[1].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}
	if b == 0 {
		return 0, true, handleDivisionByZeroError(s.ctx)
	}
	result := a / b
	if math.IsInf(result, 0) {
		return 0, true, types.ErrOverflow.GenWithStackByArgs("DOUBLE", fmt.Sprintf("(%s / %s)", s.args[0].String(), s.args[1].String()))
	}
	return result, false, nil
}

func (s *builtinArithmeticDivideDecimalSig) evalDecimal(event chunk.Event) (*types.MyDecimal, bool, error) {
	a, isNull, err := s.args[0].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}

	b, isNull, err := s.args[1].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}

	c := &types.MyDecimal{}
	err = types.DecimalDiv(a, b, c, types.DivFracIncr)
	if err == types.ErrDivByZero {
		return c, true, handleDivisionByZeroError(s.ctx)
	} else if err == types.ErrTruncated {
		sc := s.ctx.GetStochaseinstein_dbars().StmtCtx
		err = sc.HandleTruncate(errTruncatedWrongValue.GenWithStackByArgs("DECIMAL", c))
	} else if err == nil {
		_, frac := c.PrecisionAndFrac()
		if frac < s.baseBuiltinFunc.tp.Decimal {
			err = c.Round(c, s.baseBuiltinFunc.tp.Decimal, types.ModeHalfEven)
		}
	}
	return c, false, err
}

type arithmeticIntDivideFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticIntDivideFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}

	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.CausetEDN && rhsEvalTp == types.CausetEDN {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.CausetEDN, types.CausetEDN)
		if err != nil {
			return nil, err
		}
		if allegrosql.HasUnsignedFlag(lhsTp.Flag) || allegrosql.HasUnsignedFlag(rhsTp.Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		sig := &builtinArithmeticIntDivideIntSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_IntDivideInt)
		return sig, nil
	}
	bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.ETDecimal, types.ETDecimal)
	if err != nil {
		return nil, err
	}
	if allegrosql.HasUnsignedFlag(lhsTp.Flag) || allegrosql.HasUnsignedFlag(rhsTp.Flag) {
		bf.tp.Flag |= allegrosql.UnsignedFlag
	}
	sig := &builtinArithmeticIntDivideDecimalSig{bf}
	sig.setPbCode(fidelpb.ScalarFuncSig_IntDivideDecimal)
	return sig, nil
}

type builtinArithmeticIntDivideIntSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticIntDivideIntSig) Clone() builtinFunc {
	newSig := &builtinArithmeticIntDivideIntSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

type builtinArithmeticIntDivideDecimalSig struct{ baseBuiltinFunc }

func (s *builtinArithmeticIntDivideDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticIntDivideDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticIntDivideIntSig) evalInt(event chunk.Event) (int64, bool, error) {
	return s.evalIntWithCtx(s.ctx, event)
}

func (s *builtinArithmeticIntDivideIntSig) evalIntWithCtx(sctx stochastikctx.Context, event chunk.Event) (int64, bool, error) {
	b, isNull, err := s.args[1].EvalInt(sctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	if b == 0 {
		return 0, true, handleDivisionByZeroError(sctx)
	}

	a, isNull, err := s.args[0].EvalInt(sctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	var (
		ret int64
		val uint64
	)
	isLHSUnsigned := allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag)
	isRHSUnsigned := allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag)

	switch {
	case isLHSUnsigned && isRHSUnsigned:
		ret = int64(uint64(a) / uint64(b))
	case isLHSUnsigned && !isRHSUnsigned:
		val, err = types.DivUintWithInt(uint64(a), b)
		ret = int64(val)
	case !isLHSUnsigned && isRHSUnsigned:
		val, err = types.DivIntWithUint(a, uint64(b))
		ret = int64(val)
	case !isLHSUnsigned && !isRHSUnsigned:
		ret, err = types.DivInt64(a, b)
	}

	return ret, err != nil, err
}

func (s *builtinArithmeticIntDivideDecimalSig) evalInt(event chunk.Event) (ret int64, isNull bool, err error) {
	sc := s.ctx.GetStochaseinstein_dbars().StmtCtx
	var num [2]*types.MyDecimal
	for i, arg := range s.args {
		num[i], isNull, err = arg.EvalDecimal(s.ctx, event)
		if isNull || err != nil {
			return 0, isNull, err
		}
	}

	c := &types.MyDecimal{}
	err = types.DecimalDiv(num[0], num[1], c, types.DivFracIncr)
	if err == types.ErrDivByZero {
		return 0, true, handleDivisionByZeroError(s.ctx)
	}
	if err == types.ErrTruncated {
		err = sc.HandleTruncate(errTruncatedWrongValue.GenWithStackByArgs("DECIMAL", c))
	}
	if err == types.ErrOverflow {
		newErr := errTruncatedWrongValue.GenWithStackByArgs("DECIMAL", c)
		err = sc.HandleOverflow(newErr, newErr)
	}
	if err != nil {
		return 0, true, err
	}

	isLHSUnsigned := allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag)
	isRHSUnsigned := allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag)

	if isLHSUnsigned || isRHSUnsigned {
		val, err := c.ToUint()
		// err returned by ToUint may be ErrTruncated or ErrOverflow, only handle ErrOverflow, ignore ErrTruncated.
		if err == types.ErrOverflow {
			v, err := c.ToInt()
			// when the final result is at (-1, 0], it should be return 0 instead of the error
			if v == 0 && err == types.ErrTruncated {
				ret = int64(0)
				return ret, false, nil
			}
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT UNSIGNED", fmt.Sprintf("(%s DIV %s)", s.args[0].String(), s.args[1].String()))
		}
		ret = int64(val)
	} else {
		ret, err = c.ToInt()
		// err returned by ToInt may be ErrTruncated or ErrOverflow, only handle ErrOverflow, ignore ErrTruncated.
		if err == types.ErrOverflow {
			return 0, true, types.ErrOverflow.GenWithStackByArgs("BIGINT", fmt.Sprintf("(%s DIV %s)", s.args[0].String(), s.args[1].String()))
		}
	}

	return ret, false, nil
}

type arithmeticModFunctionClass struct {
	baseFunctionClass
}

func (c *arithmeticModFunctionClass) setType4ModRealOrDecimal(retTp, a, b *types.FieldType, isDecimal bool) {
	if a.Decimal == types.UnspecifiedLength || b.Decimal == types.UnspecifiedLength {
		retTp.Decimal = types.UnspecifiedLength
	} else {
		retTp.Decimal = mathutil.Max(a.Decimal, b.Decimal)
		if isDecimal && retTp.Decimal > allegrosql.MaxDecimalScale {
			retTp.Decimal = allegrosql.MaxDecimalScale
		}
	}

	if a.Flen == types.UnspecifiedLength || b.Flen == types.UnspecifiedLength {
		retTp.Flen = types.UnspecifiedLength
	} else {
		retTp.Flen = mathutil.Max(a.Flen, b.Flen)
		if isDecimal {
			retTp.Flen = mathutil.Min(retTp.Flen, allegrosql.MaxDecimalWidth)
			return
		}
		retTp.Flen = mathutil.Min(retTp.Flen, allegrosql.MaxRealWidth)
	}
}

func (c *arithmeticModFunctionClass) getFunction(ctx stochastikctx.Context, args []Expression) (builtinFunc, error) {
	if err := c.verifyArgs(args); err != nil {
		return nil, err
	}
	lhsTp, rhsTp := args[0].GetType(), args[1].GetType()
	lhsEvalTp, rhsEvalTp := numericContextResultType(lhsTp), numericContextResultType(rhsTp)
	if lhsEvalTp == types.ETReal || rhsEvalTp == types.ETReal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETReal, types.ETReal, types.ETReal)
		if err != nil {
			return nil, err
		}
		c.setType4ModRealOrDecimal(bf.tp, lhsTp, rhsTp, false)
		if allegrosql.HasUnsignedFlag(lhsTp.Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		sig := &builtinArithmeticModRealSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_ModReal)
		return sig, nil
	} else if lhsEvalTp == types.ETDecimal || rhsEvalTp == types.ETDecimal {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.ETDecimal, types.ETDecimal, types.ETDecimal)
		if err != nil {
			return nil, err
		}
		c.setType4ModRealOrDecimal(bf.tp, lhsTp, rhsTp, true)
		if allegrosql.HasUnsignedFlag(lhsTp.Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		sig := &builtinArithmeticModDecimalSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_ModDecimal)
		return sig, nil
	} else {
		bf, err := newBaseBuiltinFuncWithTp(ctx, c.funcName, args, types.CausetEDN, types.CausetEDN, types.CausetEDN)
		if err != nil {
			return nil, err
		}
		if allegrosql.HasUnsignedFlag(lhsTp.Flag) {
			bf.tp.Flag |= allegrosql.UnsignedFlag
		}
		sig := &builtinArithmeticModIntSig{bf}
		sig.setPbCode(fidelpb.ScalarFuncSig_ModInt)
		return sig, nil
	}
}

type builtinArithmeticModRealSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticModRealSig) Clone() builtinFunc {
	newSig := &builtinArithmeticModRealSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticModRealSig) evalReal(event chunk.Event) (float64, bool, error) {
	b, isNull, err := s.args[1].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	if b == 0 {
		return 0, true, handleDivisionByZeroError(s.ctx)
	}

	a, isNull, err := s.args[0].EvalReal(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	return math.Mod(a, b), false, nil
}

type builtinArithmeticModDecimalSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticModDecimalSig) Clone() builtinFunc {
	newSig := &builtinArithmeticModDecimalSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticModDecimalSig) evalDecimal(event chunk.Event) (*types.MyDecimal, bool, error) {
	a, isNull, err := s.args[0].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	b, isNull, err := s.args[1].EvalDecimal(s.ctx, event)
	if isNull || err != nil {
		return nil, isNull, err
	}
	c := &types.MyDecimal{}
	err = types.DecimalMod(a, b, c)
	if err == types.ErrDivByZero {
		return c, true, handleDivisionByZeroError(s.ctx)
	}
	return c, err != nil, err
}

type builtinArithmeticModIntSig struct {
	baseBuiltinFunc
}

func (s *builtinArithmeticModIntSig) Clone() builtinFunc {
	newSig := &builtinArithmeticModIntSig{}
	newSig.cloneFrom(&s.baseBuiltinFunc)
	return newSig
}

func (s *builtinArithmeticModIntSig) evalInt(event chunk.Event) (val int64, isNull bool, err error) {
	b, isNull, err := s.args[1].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	if b == 0 {
		return 0, true, handleDivisionByZeroError(s.ctx)
	}

	a, isNull, err := s.args[0].EvalInt(s.ctx, event)
	if isNull || err != nil {
		return 0, isNull, err
	}

	var ret int64
	isLHSUnsigned := allegrosql.HasUnsignedFlag(s.args[0].GetType().Flag)
	isRHSUnsigned := allegrosql.HasUnsignedFlag(s.args[1].GetType().Flag)

	switch {
	case isLHSUnsigned && isRHSUnsigned:
		ret = int64(uint64(a) % uint64(b))
	case isLHSUnsigned && !isRHSUnsigned:
		if b < 0 {
			ret = int64(uint64(a) % uint64(-b))
		} else {
			ret = int64(uint64(a) % uint64(b))
		}
	case !isLHSUnsigned && isRHSUnsigned:
		if a < 0 {
			ret = -int64(uint64(-a) % uint64(b))
		} else {
			ret = int64(uint64(a) % uint64(b))
		}
	case !isLHSUnsigned && !isRHSUnsigned:
		ret = a % b
	}

	return ret, false, nil
}