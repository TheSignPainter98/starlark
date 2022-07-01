package starlark_test

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
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

func TestBytesAllocations(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return `bytes(b)`, globals("b", dummyString(n))
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
		return "repr(s)", globals("s", dummyString(n))
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
		return "str(s)", globals("s", dummyString(n))
	}
	genStrFromInt := func(n uint) (string, starlark.StringDict) {
		return "str(i)", starlark.StringDict{"i": dummyInt(n)}
	}
	genStrFromBytes := func(n uint) (string, starlark.StringDict) {
		return "str(b)", globals("b", dummyBytes(n))
	}
	genStrFromList := func(n uint) (string, starlark.StringDict) {
		return "str(l)", globals("l", dummyList(n))
	}
	testAllocationsAreConstant(t, "str", genStrFromStr, 1000, 100000, 0)
	testAllocationsIncreaseLinearly(t, "str", genStrFromInt, 100, 10000, float64(starlark.UNIT_SIZE*8)/math.Log2(10))
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

func TestListAppendAllocs(t *testing.T) {
	resolve.AllowGlobalReassign = true
	gen := func(n uint) (string, starlark.StringDict) {
		return strings.Repeat("l.append('a')\n", int(n)), globals("l", starlark.NewList(nil))
	}
	testAllocationsIncreaseLinearly(t, "list.append", gen, 1000, 100000, 1)
}

func TestListExtend(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "l1.extend(l2)", globals("l1", dummyList(n), "l2", dummyList(n))
	}
	testAllocationsIncreaseLinearly(t, "list.extend", gen, 1000, 100000, 1)
}

func TestListInsert(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return strings.Repeat("l.insert(where, what)\n", int(n)), globals("l", starlark.NewList(nil), "where", -1, "what", "a")
	}
	testAllocationsIncreaseLinearly(t, "list.insert", gen, 1000, 100000, 1)
}

func TestStringCapitalize(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.capitalize()", globals("s", dummyString(n))
	}
	testAllocationsIncreaseLinearly(t, "string.capitalize", gen, 1000, 100000, 1)
}

func TestStringFormat(t *testing.T) {
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

func TestStringJoin(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.join(l)", globals("s", ",", "l", dummyList(n/2))
	}
	testAllocationsIncreaseLinearly(t, "string.join", gen, 1000, 100000, 1)
}

func TestStringLower(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.lower()", globals("s", dummyString(n))
	}
	testAllocationsIncreaseLinearly(t, "string.lower", gen, 1000, 100000, 1)
}

func TestStringPartition(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.partition('|')", globals("s", dummyString(n/2)+"|"+dummyString(n/2-1))
	}
	testAllocationsIncreaseLinearly(t, "string.partition", gen, 1000, 100000, 1)
}

func TestStringRemoveprefix(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.removeprefix(pre)", globals("s", dummyString(n), "pre", dummyString(n/2))
	}
	testAllocationsIncreaseLinearly(t, "string.removeprefix", gen, 1000, 100000, 1)
}

func TestStringRemovesuffix(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.removesuffix(suf)", globals("s", dummyString(n), "suf", dummyString(n/2))
	}
	testAllocationsIncreaseLinearly(t, "string.removeprefix", gen, 1000, 100000, 1)
}

func TestStringReplace(t *testing.T) {
	for _, expansionFac := range []float64{0.5, 1, 2} {
		gen := func(n uint) (string, starlark.StringDict) {
			return fmt.Sprintf("s.replace('aa', '%s')", strings.Repeat("b", int(expansionFac*2))), globals("s", dummyString(n))
		}
		testAllocationsIncreaseLinearly(t, "string.replace", gen, 1000, 100000, expansionFac)
	}
}

func TestStringStrip(t *testing.T) {
	whitespaceProportion := 0.5
	gen := func(n uint) (string, starlark.StringDict) {
		s := new(strings.Builder)
		s.WriteString(strings.Repeat(" ", int(float64(n)*whitespaceProportion*0.5)))
		s.WriteString(string(dummyString(uint(float64(n) * (1 - whitespaceProportion)))))
		s.WriteString(strings.Repeat(" ", int(float64(n)*whitespaceProportion*0.5)))
		return "s.strip()", globals("s", s.String())
	}
	testAllocationsIncreaseLinearly(t, "string.strip", gen, 1000, 100000, 1-whitespaceProportion)
}

func TestStringTitle(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.title()", globals("s", dummyString(n))
	}
	testAllocationsIncreaseLinearly(t, "string.title", gen, 1000, 100000, 1)
}

func TestStringUpper(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.upper()", globals("s", dummyString(n))
	}
	testAllocationsIncreaseLinearly(t, "string.title", gen, 1000, 100000, 1)
}

func TestStringSplit(t *testing.T) {
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

func TestStringSplitlines(t *testing.T) {
	for _, numLines := range []uint{0, 1, 10, 50} {
		gen := func(n uint) (string, starlark.StringDict) {
			return "s.splitlines()", globals("s", dummyLinesString(n, numLines))
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
			b.WriteString(dummyString(len / CHUNKS))
		}
	}
	return b.String()
}

func TestSetUnion(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		return "s.union(t)", globals("s", dummySet(n/2, 0), "t", dummySet(n/2, n))
	}
	testAllocationsIncreaseLinearly(t, "set.union", gen, 1000, 100000, 1)
}

type dummyType string

func (d dummyType) String() string       { return string(d) }
func (_ dummyType) Type() string         { return "dummyType" }
func (_ dummyType) Freeze()              {}
func (_ dummyType) Truth() starlark.Bool { return false }
func (d dummyType) Hash() (uint32, error) {
	return 0, fmt.Errorf("%s is not a hashable type", d.Type())
}
func (d dummyType) Unary(op syntax.Token) (starlark.Value, error) {
	return starlark.String(strings.ToUpper(string(d))), nil
}
func (x dummyType) Binary(_ syntax.Token, y starlark.Value, _ starlark.Side) (starlark.Value, error) {
	if y, ok := y.(dummyType); ok {
		return dummyType(string(x) + string(y)), nil // Concatenate regardless of binary op
	}
	return nil, nil
}

func TestStruct(t *testing.T) {
	gen := func(n uint) (string, starlark.StringDict) {
		globals := globals("fields", dummyDict(n))
		globals["struct"] = starlark.NewBuiltin("struct", starlarkstruct.Make)
		return "struct(**fields)", globals
	}
	testAllocationsIncreaseLinearly(t, "struct", gen, 1000, 100000, 2)
}

func testAllocationsAreConstant(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocs float64) {
	expectedAllocs := func(_ float64) float64 { return allocs }
	testAllocations(t, name, codeGen, nSmall, nLarge, expectedAllocs)
}

func testAllocationsIncreaseLinearly(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocsPerN float64) {
	testAllocationsIncreaseAffinely(t, name, codeGen, nSmall, nLarge, allocsPerN, 0)
}

func testAllocationsIncreaseAffinely(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, allocsPerN float64, constMinAllocs uint) {
	c := float64(constMinAllocs)
	expectedAllocs := func(n float64) float64 { return n*allocsPerN + c }
	testAllocations(t, name, codeGen, nSmall, nLarge, expectedAllocs)
}

func testAllocations(t *testing.T, name string, codeGen codeGenerator, nSmall, nLarge uint, expectedAllocsFunc func(float64) float64) {
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
		t.Errorf("memory allocations did not increase linearly: f(%d)=%d, f(%d)=%d, ratio=%.3f, want ~%.0f", nSmall, deltaSmall, nLarge, deltaLarge, ratio, expectedRatio)
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
	i = i.Lsh((len - 1) * uint(starlark.UNIT_SIZE*8))
	return i
}

func dummyString(len uint) string {
	return strings.Repeat("a", int(len))
}

func dummyLinesString(len, lines uint) string {
	if lines == 0 {
		return strings.Repeat("a", int(len))
	}
	return strings.Repeat(strings.Repeat("a", int(len/lines))+"\n", int(lines))
}

func dummyBytes(len uint) starlark.Bytes {
	return starlark.Bytes(strings.Repeat("a", int(len)))
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
