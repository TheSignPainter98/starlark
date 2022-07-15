package starlark_test

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/canonical/starlark/lib/json"
	"github.com/canonical/starlark/resolve"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/starlarkstruct"
	"github.com/canonical/starlark/syntax"
)

type codeGenerator func(n uint) (prog string, predecls starlark.StringDict)

func TestPositiveDeltaDeclaration(t *testing.T) {
	thread := new(starlark.Thread)
	thread.SetMaxAllocations(0)

	// Size increases stored
	const SIZE_INCREASE = 1000
	allocs0 := thread.Allocations()
	err := thread.DeclareSizeIncrease(SIZE_INCREASE, "TestPositiveDeltaDeclaration")
	if err != nil {
		t.Errorf("Unexpected cancellation: %v", err)
	}
	delta := thread.Allocations() - allocs0
	if delta != SIZE_INCREASE {
		t.Errorf("Incorrect size increase: expected %d but got %d", SIZE_INCREASE, delta)
	}

	// Large size increase caught
	thread.SetMaxAllocations(uintptr(SIZE_INCREASE * 1.5))
	err = thread.DeclareSizeIncrease(SIZE_INCREASE, "TestPositiveDeltaDeclaration")
	if err == nil {
		t.Errorf("Expected allocation failure!")
	}
}

func TestAllocAccountingWrapper(t *testing.T) {
	type allocWrapperTest struct {
		Name           string
		Op             func() (interface{}, error)
		ExpectedResult interface{}
		ExpectedDelta  uintptr
		ExpectOpDone   bool
		Prealloc       uintptr
		ResultSizeOf   starlark.Sizer
		ExpectPass     bool
		MaxAllocations uintptr
	}

	dummyStrLen := uint(100)
	dummyStr := dummyString(dummyStrLen, 'a')
	strlenSizeOf := func(s interface{}) uintptr { return 1 + uintptr(len(s.(string))) }
	expectedDummyStrDelta := strlenSizeOf(dummyStr)
	opDone := false

	accountingTests := []allocWrapperTest{
		{ // Ensure ok when no allocations made
			Name:           "no-allocs",
			Op:             func() (interface{}, error) { opDone = true; return nil, nil },
			ExpectedResult: nil,
			ExpectedDelta:  0,
			ExpectOpDone:   true,
			Prealloc:       0,
			ResultSizeOf:   nil,
			ExpectPass:     true,
			MaxAllocations: 1,
		},
		{ // Ensure ok when a small number of preallocations declared
			Name:           "ok-preallocs",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: dummyStr,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   true,
			Prealloc:       strlenSizeOf(dummyStr),
			ResultSizeOf:   nil,
			ExpectPass:     true,
			MaxAllocations: 2 * uintptr(dummyStrLen),
		},
		{ // Ensure ok when a small number of post-allocations are detected
			Name:           "ok-postallocs",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: dummyStr,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   true,
			Prealloc:       0,
			ResultSizeOf:   strlenSizeOf,
			ExpectPass:     true,
			MaxAllocations: 2 * uintptr(dummyStrLen),
		},
		{ // Ensure cancellation when too many allocations are requested before the operation
			Name:           "pre-fail",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: nil,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   false,
			Prealloc:       strlenSizeOf(dummyStr),
			ResultSizeOf:   nil,
			ExpectPass:     false,
			MaxAllocations: 1,
		},
		{ // Ensure cancellation when too many allocations detected after operation
			Name:           "post-fail",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: nil,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   true,
			Prealloc:       0,
			ResultSizeOf:   strlenSizeOf,
			ExpectPass:     false,
			MaxAllocations: 1,
		},
		{ // Ensure ok when more allocations detected in result than the non-zero preallocated, and where the sum of these two values would be too many allocations
			Name:           "ok-prealloc-and-sizer-increase",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: dummyStr,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   true,
			Prealloc:       uintptr(0.5 * float64(dummyStrLen)),
			ResultSizeOf:   strlenSizeOf,
			ExpectPass:     true,
			MaxAllocations: uintptr(1.25 * float64(strlenSizeOf(dummyStr))),
		},
		{ // Ensure ok when fewer allocations detected in result than the non-zero preallocated, and where the sum of these two values would be too many allocations
			Name:           "ok-prealloc-and-sizer-decrease",
			Op:             func() (interface{}, error) { opDone = true; return dummyStr, nil },
			ExpectedResult: dummyStr,
			ExpectedDelta:  expectedDummyStrDelta,
			ExpectOpDone:   true,
			Prealloc:       uintptr(1.2 * float64(dummyStrLen)),
			ResultSizeOf:   strlenSizeOf,
			ExpectPass:     true,
			MaxAllocations: uintptr(1.25 * float64(strlenSizeOf(dummyStr))),
		},
	}
	for _, test := range accountingTests {
		opDone = false
		thread := new(starlark.Thread)
		thread.SetMaxAllocations(test.MaxAllocations)
		result, err := starlark.AccountAllocsForOperation(thread, test.Name, test.Op, test.Prealloc, test.ResultSizeOf)
		if result != test.ExpectedResult {
			t.Errorf("%s: Incorrect result: expected %v but got %v", test.Name, test.ExpectedResult, result)
		}
		if test.ExpectPass && err != nil {
			t.Errorf("%s: Unexpected error: %v", test.Name, err)
		} else if !test.ExpectPass && err == nil {
			t.Errorf("%s: Expected error but test incorrectly passed", test.Name)
		}
		if thread.Allocations() != test.ExpectedDelta {
			t.Errorf("%s: Expected %d allocations but got %d instead", test.Name, test.ExpectedDelta, thread.Allocations())
		}
		if result != nil && err != nil {
			t.Errorf("%s: Expected either a nil result or a nil error", test.Name)
		}
		if opDone != test.ExpectOpDone {
			notStr := func(b bool) string {
				if b {
					return ""
				} else {
					return " not"
				}
			}
			t.Errorf("%s: Expected operation to%s be done, but operation was%s done", test.Name, notStr(test.ExpectOpDone), notStr(opDone))
		}
	}
}

func TestBytesAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return `bytes(b)`, globals("b", dummyString(n, 'b'))
	}
	testAllocationsIncreaseLinearly(t, "bytes", gen, 1000, 100000, 1)
}

func TestDictAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "dict(**fields)", globals("fields", dummyDict(n))
	}
	testAllocationsIncreaseLinearly(t, "dict", gen, 25, 250, 1)
}

func TestEnumerateAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "enumerate(e)", globals("e", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "enumerate", gen, 1000, 100000, 1)
}

func TestListAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "list(l)", globals("l", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "list", gen, 25, 255, 1)
}

func TestReprAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "repr(s)", globals("s", dummyString(n, 's'))
	}
	testAllocationsIncreaseLinearly(t, "repr", gen, 1000, 100000, 1)
}

func TestSetAllocations(t *testing.T) {
	resolve.AllowSet = true
	gen := func(n uint) (string, starlark.StringDict) {
		return "set(l)", globals("l", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "set", gen, 1000, 100000, 1)
}

func TestStrAllocations(t *testing.T) {
	genStrFromStr := func(n uint) (string, starlark.StringDict) {
		return "str(s)", globals("s", dummyString(n, 'a'))
	}
	genStrFromInt := func(n uint) (string, starlark.StringDict) {
		return "str(i)", starlark.StringDict{"i": dummyInt(n)}
	}
	genStrFromBytes := func(n uint) (string, starlark.StringDict) {
		return "str(b)", globals("b", dummyBytes(n, 'a'))
	}
	genStrFromList := func(n uint) (string, starlark.StringDict) {
		return "str(l)", globals("l", dummyList(n))
	}
	testAllocationsAreConstant(t, "str", genStrFromStr, 1000, 100000, 0)
	testAllocationsIncreaseLinearly(t, "str", genStrFromInt, 1000, 100000, 1/math.Log2(10))
	testAllocationsIncreaseLinearly(t, "str", genStrFromBytes, 1000, 100000, 1)
	testAllocationsIncreaseLinearly(t, "str", genStrFromList, 1000, 100000, float64(len(`"a", `)))
}

func TestTupleAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "tuple(l)", globals("l", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "tuple", gen, 1000, 100000, 1)
}

func TestZipAllocations(t *testing.T) {
	genZipCall := func(m uint) string {
		entries := make([]string, 0, m)
		for i := uint(1); i <= m; i++ {
			entries = append(entries, fmt.Sprintf("l%d", i))
		}
		return fmt.Sprintf("zip(%s)", strings.Join(entries, ", "))
	}
	genZipGlobals := func(n, m uint) starlark.StringDict {
		globals := make(starlark.StringDict, m)
		for i := uint(1); i <= m; i++ {
			globals[fmt.Sprintf("l%d", i)] = dummyList(n / m)
		}
		return globals
	}
	genPairZip := func(n uint) (string, starlark.StringDict) {
		return genZipCall(2), genZipGlobals(n, 2)
	}
	genQuintZip := func(n uint) (string, starlark.StringDict) {
		return genZipCall(5), genZipGlobals(n, 5)
	}
	genCollatingZip := func(n uint) (string, starlark.StringDict) {
		return genZipCall(n), genZipGlobals(n, n)
	}
	testAllocationsIncreaseLinearly(t, "zip", genPairZip, 1000, 100000, 1.5) // Allocates backing array
	testAllocationsIncreaseLinearly(t, "zip", genQuintZip, 1000, 100000, 1.2)
	testAllocationsIncreaseAffinely(t, "zip", genCollatingZip, 10, 255, 1, 3)
}

func TestDictItemsAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "d.items()", globals("d", dummyDict(n))
	}
	testAllocationsIncreaseLinearly(t, "dict.items", gen, 1000, 100000, 1)
}

func TestDictKeysAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "d.keys()", globals("d", dummyDict(n))
	}
	testAllocationsIncreaseLinearly(t, "dict.keys", gen, 1000, 100000, 1)
}

func TestDictValuesAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "d.values()", globals("d", dummyDict(n))
	}
	testAllocationsIncreaseLinearly(t, "dict.values", gen, 1000, 100000, 1)
}

func TestListAppendAllocations(t *testing.T) {
	resolve.AllowGlobalReassign = true
	gen := func(n uint) (string, starlark.StringDict) {
		return strings.Repeat("l.append('a')\n", int(n)), globals("l", starlark.NewList(nil))
	}
	testAllocationsIncreaseLinearly(t, "list.append", gen, 1000, 100000, 1)
}

func TestListExtendAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "l1.extend(l2)", globals("l1", dummyList(n), "l2", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "list.extend", gen, 1000, 100000, 1)
}

func TestListInsertAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return strings.Repeat("l.insert(where, what)\n", int(n)), globals("l", starlark.NewList(nil), "where", -1, "what", "a")
	}
	testAllocationsIncreaseLinearly(t, "list.insert", gen, 1000, 100000, 1)
}

func TestStringCapitalizeAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.capitalize()", globals("s", dummyString(n, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.capitalize", gen, 1000, 100000, 1)
}

func TestStringFormatAllocations(t *testing.T) {
	genNoFmt := func(n uint) (string, starlark.StringDict) {
		return "s.format()", globals("s", strings.Repeat("{{}}", int(n/4)))
	}
	genFmtStrings := func(n uint) (string, starlark.StringDict) {
		return "s.format(*l)", globals("s", strings.Repeat("{}", int(n/2)), "l", dummyList(n/2))
	}
	genFmtInts := func(n uint) (string, starlark.StringDict) {
		ints := make([]starlark.Value, 0, n/2)
		for i := uint(0); i < n/2; i++ {
			ints = append(ints, starlark.MakeInt(0))
		}
		return "s.format(*l)", globals("s", strings.Repeat("{}", int(n/2)), "l", ints)
	}
	testAllocationsIncreaseLinearly(t, "string.format", genNoFmt, 1000, 100000, 0.5)
	testAllocationsIncreaseLinearly(t, "string.format", genFmtStrings, 1000, 100000, 0.5)
	testAllocationsIncreaseLinearly(t, "string.format", genFmtInts, 1000, 100000, 0.5)
}

func TestStringJoinAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.join(l)", globals("s", ",", "l", dummyList(n/2))
	}
	testAllocationsIncreaseLinearly(t, "string.join", gen, 1000, 100000, 1)
}

func TestStringLowerAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.lower()", globals("s", dummyString(n, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.lower", gen, 1000, 100000, 1)
}

func TestStringPartitionAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.partition('|')", globals("s", dummyString(n/2, 's')+"|"+dummyString(n/2-1, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.partition", gen, 1000, 100000, 1)
}

func TestStringRemoveprefixAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.removeprefix(pre)", globals("s", dummyString(n, 's'), "pre", dummyString(n/2, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.removeprefix", gen, 1000, 100000, 1)
}

func TestStringRemovesuffixAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.removesuffix(suf)", globals("s", dummyString(n, 's'), "suf", dummyString(n/2, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.removeprefix", gen, 1000, 100000, 1)
}

func TestStringReplaceAllocations(t *testing.T) {
	for _, expansionFac := range []float64{0.5, 1, 2} {
		gen := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("s.replace('aa', '%s')", strings.Repeat("b", int(expansionFac*2))), globals("s", dummyString(n, 'a'))
		}
		testAllocationsIncreaseLinearly(t, "string.replace", gen, 1000, 100000, expansionFac)
	}
}

func TestStringStripAllocations(t *testing.T) {
	whitespaceProportion := 0.5
	gen := func(n uint) (string, starlark.StringDict) {
		s := new(strings.Builder)
		s.WriteString(strings.Repeat(" ", int(float64(n)*whitespaceProportion*0.5)))
		s.WriteString(string(dummyString(uint(float64(n)*(1-whitespaceProportion)), 'a')))
		s.WriteString(strings.Repeat(" ", int(float64(n)*whitespaceProportion*0.5)))
		return "s.strip()", globals("s", s.String())
	}
	testAllocationsIncreaseLinearly(t, "string.strip", gen, 1000, 100000, 1-whitespaceProportion)
}

func TestStringTitleAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.title()", globals("s", dummyString(n, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.title", gen, 1000, 100000, 1)
}

func TestStringUpperAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.upper()", globals("s", dummyString(n, 's'))
	}
	testAllocationsIncreaseLinearly(t, "string.title", gen, 1000, 100000, 1)
}

func TestStringSplitAllocations(t *testing.T) {
	for _, sep := range []string{"", " ", "|"} {
		gen := func(n uint) (string, starlark.StringDict) {
			passSep := &sep
			if sep == "" {
				passSep = nil
			}
			return "s.split(sep)", globals("s", generateSepString(n, sep), "sep", passSep)
		}
		testAllocationsIncreaseLinearly(t, "string.split", gen, 1000, 100000, 1)
	}
}

func TestStringSplitlinesAllocations(t *testing.T) {
	for _, numLines := range []uint{0, 1, 10, 50} {
		gen := func(n uint) (string, starlark.StringDict) {
			return "s.splitlines()", globals("s", dummyLinesString(n, numLines, 'a'))
		}
		testAllocationsIncreaseLinearly(t, "string.splitlines", gen, 1000, 100000, 1)
	}
}

func generateSepString(len uint, sep string) string {
	b := new(strings.Builder)
	b.Grow(int(len))
	{
		const CHUNKS = 10
		for i := 0; i < CHUNKS; i++ {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(dummyString(len/CHUNKS, 'a'))
		}
	}
	return b.String()
}

func TestSetUnionAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.union(t)", globals("s", dummySet(n/2, 0), "t", dummySet(n/2, n))
	}
	testAllocationsIncreaseLinearly(t, "set.union", gen, 1000, 100000, 1)
}

type dummyType struct{ s string }
type dummyTypeIterator struct{ *dummyType }

var (
	_ starlark.Value            = new(dummyType)
	_ starlark.HasSizedUnary    = new(dummyType)
	_ starlark.HasSizedBinary   = new(dummyType)
	_ starlark.HasSizedIndex    = new(dummyType)
	_ starlark.HasSizedSetIndex = new(dummyType)
	_ starlark.Iterable         = new(dummyType)
	_ starlark.Iterator         = new(dummyTypeIterator)
	_ starlark.HasSizedNext     = new(dummyTypeIterator)
)

func (d *dummyType) String() string       { return string(d.s) }
func (_ *dummyType) Type() string         { return "dummyType" }
func (_ *dummyType) Freeze()              {}
func (_ *dummyType) Truth() starlark.Bool { return false }
func (d *dummyType) Hash() (uint32, error) {
	return 0, fmt.Errorf("%s is not a hashable type", d.Type())
}
func (d *dummyType) Unary(op syntax.Token) (starlark.Value, error) {
	return starlark.String(strings.ToUpper(string(d.s))), nil
}
func (d *dummyType) UnarySizer(_ syntax.Token) (uintptr, starlark.Sizer) {
	return 1 + uintptr(len(d.s)), nil
}
func (x *dummyType) Binary(_ syntax.Token, y starlark.Value, _ starlark.Side) (starlark.Value, error) {
	if y, ok := y.(*dummyType); ok {
		return &dummyType{string(x.s) + string(y.s)}, nil // Concatenate regardless of binary op
	}
	return nil, nil
}
func (x *dummyType) BinarySizer(_ syntax.Token, y starlark.Value, _ starlark.Side) (uintptr, starlark.Sizer) {
	if y, ok := y.(*dummyType); ok {
		return 2 + uintptr(len(x.s)+len(y.s)), nil
	}
	return 0, nil
}
func (d *dummyType) Index(_ int) starlark.Value {
	return &dummyType{d.s[:]}
}
func (d *dummyType) Len() int {
	return len(d.s)
}
func (d *dummyType) IndexSizer(_ int) (uintptr, starlark.Sizer) {
	return 2 + uintptr(len(d.s)), nil
}
func (d *dummyType) SetIndex(_ int, v starlark.Value) error {
	*d = dummyType{d.s[:]}
	return nil
}
func (_ *dummyType) SetIndexSizer(_ int, _ starlark.Value) (uintptr, starlark.Sizer) {
	return 0, func(r interface{}) uintptr {
		return uintptr(len(r.(*dummyType).s))
	}
}
func (dt *dummyType) Iterate() starlark.Iterator {
	return &dummyTypeIterator{dt}
}

func (it *dummyTypeIterator) Next(p *starlark.Value) (true bool) {
	*p = &dummyType{it.s[:]}
	return
}
func (it *dummyTypeIterator) Done() {}
func (it *dummyTypeIterator) NextSizer() (uintptr, starlark.Sizer) {
	return 0, func(v interface{}) uintptr { return uintptr(1 + len(v.(*dummyType).s)) }
}

func TestInterpLoopUnaryAllocations(t *testing.T) {
	for _, op := range []string{"-", "~"} {
		genInt := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("%sa", op), globals("a", dummyInt(n))
		}
		genCustom := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("%sa", op), globals("a", &dummyType{dummyString(n, 'a')})
		}
		testAllocationsIncreaseLinearly(t, "unary", genInt, 1000, 100000, 1/float64(8*starlark.UNIT_SIZE))
		testAllocationsIncreaseLinearly(t, "unary", genCustom, 1000, 100000, 1)
	}
}

func TestInterpLoopBinaryAllocations(t *testing.T) {
	genIntsWithOp := func(op string) codeGenerator {
		return func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("a %s b", op), globals("a", dummyInt(n), "b", dummyInt(n/2))
		}
	}

	opIntAllocs := map[string]float64{
		"+":  1,
		"-":  1,
		"*":  1.5,
		"/":  1,
		"//": 0.5,
		"%":  0.5,
		"&":  1,
		"|":  1,
		"^":  1,
	}

	for _, op := range []string{"+", "-", "*", "//", "%", "&", "|", "^"} {
		genCustoms := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("a %s b", op), globals("a", &dummyType{dummyString(n/2, 'a')}, "b", &dummyType{dummyString(n/2, 'b')})
		}
		testAllocationsIncreaseLinearly(t, "binary", genIntsWithOp(op), 10000, 100000, opIntAllocs[op]/float64(8*starlark.UNIT_SIZE))
		testAllocationsIncreaseLinearly(t, "binary", genCustoms, 1000, 100000, 1)
	}
	testAllocationsAreConstant(t, "binary", genIntsWithOp("/"), 100, 1000, opIntAllocs["/"])
}

func TestInterpLoopInplaceBinaryAllocations(t *testing.T) {
	resolve.AllowGlobalReassign = true

	genIntsWithOp := func(op string) codeGenerator {
		return func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("c = a; c %s= b", op), globals("a", dummyInt(n), "b", dummyInt(n/2))
		}
	}

	opIntAllocs := map[string]float64{
		"+":  1,
		"-":  1,
		"*":  1.5,
		"/":  1,
		"//": 0.5,
		"%":  0.5,
		"&":  1,
		"|":  1,
		"^":  1,
	}

	for _, op := range []string{"+", "-", "*", "//", "%", "&", "|", "^"} {
		genCustoms := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("c = a; c %s= b", op), globals("a", &dummyType{dummyString(n/2, 'a')}, "b", &dummyType{dummyString(n/2, 'b')})
		}

		testAllocationsIncreaseLinearly(t, "inplace_binary", genIntsWithOp(op), 10000, 100000, opIntAllocs[op]/float64(8*starlark.UNIT_SIZE))
		testAllocationsIncreaseLinearly(t, "inplace_binary", genCustoms, 1000, 100000, 1)
	}
	testAllocationsAreConstant(t, "binary", genIntsWithOp("/"), 100, 1000, opIntAllocs["/"])
}

func TestInterpLoopIndexAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "d[i]", globals("d", &dummyType{dummyString(n, 'a')}, "i", 1)
	}
	testAllocationsIncreaseLinearly(t, "index", gen, 1000, 100000, 1)
}

func TestInterpLoopSetIndexAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "d[i] = v", globals("d", &dummyType{dummyString(n, 'a')}, "i", 1, "v", -2)
	}
	testAllocationsIncreaseLinearly(t, "index", gen, 1000, 100000, 1)
}

type dummyIterable struct{ max uint }
type dummyIterator struct{ curr, len uint }

var _ starlark.Value = (*dummyIterable)(nil)
var _ starlark.Iterable = (*dummyIterable)(nil)
var _ starlark.Iterator = (*dummyIterator)(nil)

func (d dummyIterable) String() string       { return fmt.Sprint(d.max) }
func (_ dummyIterable) Type() string         { return "dummyType" }
func (_ dummyIterable) Freeze()              {}
func (_ dummyIterable) Truth() starlark.Bool { return false }
func (d dummyIterable) Hash() (uint32, error) {
	return 0, fmt.Errorf("%s is not a hashable type", d.Type())
}

func (d *dummyIterable) Iterate() starlark.Iterator {
	return &dummyIterator{0, d.max}
}
func (it *dummyIterator) Next(p *starlark.Value) bool {
	if it.curr < it.len {
		*p = starlark.MakeInt(int(it.curr))
		it.curr++
		return true
	}
	return false
}
func (*dummyIterator) Done() {}

func TestInterpLoopSetDictAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "{i:i for i in r}", globals("r", &dummyIterable{n})
	}
	testAllocationsIncreaseLinearly(t, "setdict", gen, 1000, 100000, 1)
}

func TestInterpLoopSetDictUniqAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		dictElems := new(strings.Builder)
		es := make([]starlark.Value, n)
		for i := uint(0); i < n; i++ {
			dictElems.WriteString(fmt.Sprintf("es[%d]:es[%d],", i, i))
			es[i] = starlark.String(fmt.Sprintf("_%d", i))
		}
		return fmt.Sprintf("{%s}", dictElems.String()), globals("es", es)
	}
	testAllocationsIncreaseLinearly(t, "setdictuniq", gen, 1000, 100000, 1)
}

func TestInterpLoopAppendAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "[i for i in r]", globals("r", &dummyIterable{n})
	}
	testAllocationsIncreaseLinearly(t, "append", gen, 1000, 100000, 1)
}

func TestInterpLoopSliceAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		l := make([]starlark.Value, n)
		for i := uint(0); i < n; i++ {
			l[i] = starlark.String(fmt.Sprintf("_%d", i))
		}
		return strings.Repeat("l[lo:hi:step]\n", int(n)), globals("l", l, "lo", 0, "hi", n, "step", n)
	}
	testAllocationsIncreaseLinearly(t, "slice", gen, 1000, 100000, 1)
}

func TestInterpLoopMakeTupleAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		globals := make(starlark.StringDict, n)
		listContents := new(strings.Builder)
		listContents.Grow((len("_,") + int(math.Log2(float64(n)))) * int(n))
		for i := uint(0); i < n; i++ {
			s := fmt.Sprintf("_%d", i)
			globals[s] = starlark.String(s)
			listContents.WriteString(s + ",")
		}
		return fmt.Sprintf("s = (%s)", listContents.String()), globals
	}
	testAllocationsIncreaseLinearly(t, "maketuple", gen, 1000, 100000, 1)
}

func TestInterpLoopMakeListAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		globals := make(starlark.StringDict, n)
		listContents := new(strings.Builder)
		listContents.Grow((len("_,") + int(math.Log2(float64(n)))) * int(n))
		for i := uint(0); i < n; i++ {
			s := fmt.Sprintf("_%d", i)
			globals[s] = starlark.String(s)
			listContents.WriteString(s + ",")
		}
		return fmt.Sprintf("s = [%s]", listContents.String()), globals
	}
	testAllocationsIncreaseLinearly(t, "makelist", gen, 1000, 100000, 1)
}

func TestInterpLoopSetIndexAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		prog := new(strings.Builder)
		prog.Grow((len("d[] = \n") + int(2*math.Log2(float64(n)))) * int(n))
		globals := make(starlark.StringDict, n+1)
		globals["d"] = starlark.NewDict(int(n))
		for i := uint(0); i < n; i++ {
			s := fmt.Sprintf("_%d", i)
			globals[s] = starlark.String(s)
			prog.WriteString(fmt.Sprintf("d[%s] = %s\n", s, s))
		}
		return prog.String(), globals
	}
	testAllocationsIncreaseLinearly(t, "setindex", gen, 1000, 100000, 1)

	genNonUnique := func(n uint) (string, starlark.StringDict) {
		prog := new(strings.Builder)
		prog.Grow(len("d[e] = e\n") * int(n))
		for i := uint(0); i < n; i++ {
			prog.WriteString("d[e] = e\n")
		}
		return prog.String(), globals("d", starlark.NewDict(1), "e", starlark.String("_e"))
	}

	testAllocationsAreConstant(t, "setindex", genNonUnique, 1000, 100000, 1)
}

func TestInterpLoopMakeFuncAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		prog := new(strings.Builder)
		funcCode := "def func_%d():\n\tprint('Hello, world!')\n"
		prog.Grow(int(n) * (len(funcCode) + int(math.Log2(float64(n)))))
		for i := uint(0); i < n; i++ {
			prog.WriteString(fmt.Sprintf(funcCode, i))
		}
		return prog.String(), nil
	}
	testAllocationsIncreaseLinearly(t, "makefunc", gen, 1000, 100000, 2)
}

func TestInterpLoopMakeDictAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return strings.Repeat("s = {}\n", int(n)), nil
	}
	testAllocationsIncreaseLinearly(t, "makedict", gen, 1000, 100000, 1)
}

func TestStructAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		globals := globals("fields", dummyDict(n))
		globals["struct"] = starlark.NewBuiltin("struct", starlarkstruct.Make)
		return "struct(**fields)", globals
	}
	testAllocationsIncreaseLinearly(t, "struct", gen, 1000, 100000, 2)
}

func TestLibJsonEncodeAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		list := make([]starlark.Value, 0, n)
		for i := uint(0); i < n; i++ {
			list = append(list, starlark.String("a"))
		}
		globals := globals("json", json.Module, "l", list)
		return "json.encode(l)", globals
	}
	testAllocationsIncreaseLinearly(t, "json.encode", gen, 1000, 100000, float64(len(`"a",`)))
}

func TestLibJsonIndentAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		list := new(strings.Builder)
		list.WriteString(`["a"`)
		for i := uint(0); i < n-1; i++ {
			list.WriteString(`,"a"`)
		}
		list.WriteRune(']')
		return "json.indent(s)", globals("json", json.Module, "s", list.String())
	}
	testAllocationsIncreaseLinearly(t, "json.indent", gen, 1000, 100000, float64(len("	\"a\",\n")))
}

func TestLibJsonDecodeAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		list := new(strings.Builder)
		list.WriteString(`["a"`)
		for i := uint(0); i < n; i++ {
			list.WriteString(`,"a"`)
		}
		list.WriteRune(']')
		return "json.decode(l)", globals("json", json.Module, "l", list.String())
	}
	testAllocationsIncreaseLinearly(t, "json.decode", gen, 1000, 100000, 3)
}

func testAllocationsAreConstant(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocs float64) {
	expectedAllocs := func(_ float64) float64 { return allocs }
	testAllocations(t, name, codeGen, nSmall, nLarge, expectedAllocs, "remain constant")
}

func testAllocationsIncreaseLinearly(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocsPerN float64) {
	testAllocationsIncreaseAffinely(t, name, codeGen, nSmall, nLarge, allocsPerN, 0)
}

func testAllocationsIncreaseAffinely(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocsPerN float64, constMinAllocs uint) {
	c := float64(constMinAllocs)
	expectedAllocs := func(n float64) float64 { return n*allocsPerN + c }
	testAllocations(t, name, codeGen, nSmall, nLarge, expectedAllocs, "increase linearly")
}

func testAllocations(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, expectedAllocsFunc func(float64) float64, trendName string) {
	thread := new(starlark.Thread)
	thread.SetMaxAllocations(0)
	file := name + ".star"

	// Test allocation increase order
	codeSmall, predeclSmall := codeGen(nSmall)
	deltaSmall, err := memoryIncrease(thread, file, codeSmall, predeclSmall)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	codeLarge, predeclLarge := codeGen(nLarge)
	deltaLarge, err := memoryIncrease(thread, file, codeLarge, predeclLarge)
	if err != nil {
		t.Errorf("Unexpected error %v", err)
	}
	ratio := float64(deltaLarge) / float64(deltaSmall)
	expectedRatio := expectedAllocsFunc(float64(nLarge)) / expectedAllocsFunc(float64(nSmall))
	if ratio <= 0.9*expectedRatio || 1.1*expectedRatio <= ratio {
		t.Errorf("memory allocations did not %s: f(%d)=%d, f(%d)=%d, ratio=%.3f, want ~%.0f", trendName, nSmall, deltaSmall, nLarge, deltaLarge, ratio, expectedRatio)
	}

	// Test allocations are roughly correct
	expectedAllocs := expectedAllocsFunc(float64(nLarge))
	expectedMinAllocs := uintptr(0.9 * expectedAllocs)
	expectedMaxAllocs := uintptr(1.1 * expectedAllocs)
	if deltaLarge < expectedMinAllocs {
		t.Errorf("Too few allocations, expected ~%.0f but used only %d", expectedAllocs, deltaLarge)
	}
	if expectedMaxAllocs < deltaLarge {
		t.Errorf("Too many allocations, expected ~%.0f but used %d", expectedAllocs, deltaLarge)
	}
}

func memoryIncrease(thread *starlark.Thread, name, code string, predeclared starlark.StringDict) (uintptr, error) {
	allocs0 := thread.Allocations()
	_, err := starlark.ExecFile(thread, name, code, predeclared)
	return thread.Allocations() - allocs0, err
}

func dummyInt(len uint) starlark.Int {
	i := starlark.MakeInt(1)
	i = i.Lsh(len - 1)
	return i
}

func dummyString(len uint, char rune) string {
	return strings.Repeat(string(char), int(len))
}

func dummyLinesString(len, lines uint, char rune) string {
	if lines == 0 {
		return strings.Repeat(string(char), int(len))
	}
	return strings.Repeat(strings.Repeat("a", int(len/lines))+"\n", int(lines))
}

func dummyBytes(len uint, char rune) starlark.Bytes {
	return starlark.Bytes(strings.Repeat(string(char), int(len)))
}

func dummyList(len uint) *starlark.List {
	elems := make([]starlark.Value, 0, len)
	for i := uint(0); i < len; i++ {
		elems = append(elems, starlark.String("a"))
	}
	return starlark.NewList(elems)
}

func dummySet(len, start uint) *starlark.Set {
	set := starlark.NewSet(int(len))
	for i := uint(0); i < len; i++ {
		set.Insert(starlark.MakeInt(int(start + i)))
	}
	return set
}

func dummyDict(len uint) *starlark.Dict {
	dict := starlark.NewDict(int(len))
	for i := 1; i <= int(len); i++ {
		s := starlark.String(strconv.Itoa(i))
		dict.SetKey("_"+s, s)
	}
	return dict
}

func globals(gs ...interface{}) starlark.StringDict {
	if len(gs)%2 != 0 {
		panic("globals requires an even number of arguments")
	}

	globals := make(starlark.StringDict, len(gs)/2)
	for i := 1; i < len(gs); i += 2 {
		key := gs[i-1].(string)
		switch val := gs[i].(type) {
		case starlark.Value:
			globals[key] = val
		case []starlark.Value:
			globals[key] = starlark.NewList(val)
		case string:
			globals[key] = starlark.String(val)
		case *string:
			if val == nil {
				globals[key] = starlark.None
				continue
			}
			globals[key] = starlark.String(*val)
		case uint:
			globals[key] = starlark.MakeInt(int(val))
		case int:
			globals[key] = starlark.MakeInt(val)
		case float64:
			globals[key] = starlark.Float(val)
		default:
			panic(fmt.Sprintf("Could not coerce %v into a starlark value", val))
		}
	}
	return globals
}
