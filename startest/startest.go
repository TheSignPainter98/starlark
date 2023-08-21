// Package startest provides a framework to test Starlark code, environments
// and their safety.
//
// This framework is designed to hook into existing test frameworks, such as
// testing and go-check, so it can be used to write unit tests for Starlark
// usage.
//
// When a test is run, the startest instance exposes an integer N which must be
// used to scale the total resources used by the test. All checks are done in
// terms of this N, so for example, calling SetMaxAllocs(100) on a startest
// instance will cause it to check that no more than 100 bytes are allocated
// per given N. Tests are repeated with different values of N to reduce the
// effect of noise on measurements.
//
// To create a new startest instance, use From. To test a string of Starlark
// code, use the instances's RunString method. To directly test Starlark (or
// something more expressible in Go), use the RunThread method. To simulate the
// running environment of a Starlark script, use the AddValue, AddBuiltin and
// AddLocal methods. All safety conditions are required by default; to instead
// test a specific subset of safety conditions, use the RequireSafety method.
// To test resource usage, use the SetMaxAllocs method. To count the memory
// cost of a value in a test, use the KeepAlive method. The Error, Errorf,
// Fatal, Fatalf, Log and Logf methods are inherited from the test's base.
//
// When executing Starlark code, the startest instance can be accessed through
// the global st. To access the exposed N, use st.n. To count the memory cost
// of a particular value, use st.keep_alive. To report errors, use st.error or
// st.fatal. To write to the log, use the print builtin. To ergonomically make
// assertions, use the provided assert global which provides functions such as
// assert.eq, assert.true and assert.fails.
package startest

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/canonical/starlark/resolve"
	"github.com/canonical/starlark/starlark"
	"github.com/canonical/starlark/starlarktest"
	"github.com/canonical/starlark/syntax"
	"gopkg.in/check.v1"
)

type TestBase interface {
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Failed() bool
	Log(args ...interface{})
	Logf(fmt string, args ...interface{})
}

type ST struct {
	maxAllocs          uint64
	maxExecutionSteps  uint64
	alive              []interface{}
	N                  int
	requiredSafety     starlark.Safety
	safetyGiven        bool
	predecls           starlark.StringDict
	locals             map[string]interface{}
	executionStepModel string
	TestBase
}

const stSafe = starlark.CPUSafe | starlark.MemSafe | starlark.TimeSafe | starlark.IOSafe

var _ starlark.Value = &ST{}
var _ starlark.HasAttrs = &ST{}

var _ TestBase = &testing.T{}
var _ TestBase = &testing.B{}
var _ TestBase = &check.C{}

// From returns a new starTest instance with a given test base.
func From(base TestBase) *ST {
	return &ST{
		TestBase:          base,
		maxAllocs:         math.MaxUint64,
		maxExecutionSteps: math.MaxUint64,
	}
}

// SetMaxAllocs optionally sets the max allocations allowed per st.N.
func (st *ST) SetMaxAllocs(maxAllocs uint64) {
	st.maxAllocs = maxAllocs
}

// SetMaxExecutionSteps optionally sets the max execution steps allowed per st.N.
func (st *ST) SetMaxExecutionSteps(maxExecutionSteps uint64) {
	st.maxExecutionSteps = maxExecutionSteps
}

// RequireSafety optionally sets the required safety of tested code.
func (st *ST) RequireSafety(safety starlark.Safety) {
	st.requiredSafety |= safety
	st.safetyGiven = true
}

// AddValue makes the given value accessible under the given name in the
// Starlark environment used by RunString.
func (st *ST) AddValue(name string, value starlark.Value) {
	if value == nil {
		st.Errorf("AddValue expected a value: got %T", value)
		return
	}

	st.addValueUnchecked(name, value)
}

// AddBuiltin makes the given builtin available under the name specified in its
// Name method in the Starlark environment used by RunString.
func (st *ST) AddBuiltin(fn starlark.Value) {
	builtin, ok := fn.(*starlark.Builtin)
	if !ok {
		st.Errorf("AddBuiltin expected a builtin: got %v", fn)
		return
	}

	st.addValueUnchecked(builtin.Name(), builtin)
}

func (st *ST) addValueUnchecked(name string, value starlark.Value) {
	if st.predecls == nil {
		st.predecls = make(starlark.StringDict)
	}
	st.predecls[name] = value
}

// AddLocal adds the given object into the local values available to spawned
// threads.
func (st *ST) AddLocal(name string, value interface{}) {
	if st.locals == nil {
		st.locals = make(map[string]interface{})
	}
	st.locals[name] = value
}

// TODO: rename
func sourceCode(filename string, code string, isPredeclared func(string) bool) (*syntax.File, *starlark.Program, error) {
	code, err := Reindent(code)
	if err != nil {
		return nil, nil, err
	}

	allowGlobalReassign := resolve.AllowGlobalReassign
	defer func() {
		resolve.AllowGlobalReassign = allowGlobalReassign
	}()
	resolve.AllowGlobalReassign = true

	return starlark.SourceProgram("startest.RunString", code, isPredeclared)
}

// TODO: rename this!
// name ideas:
// - SetReferenceImplementation
// - SetExecutionModel
// - SetStepModel
// - SetExecutionStepModel
func (st *ST) SetExecutionStepModel(code string) {
	st.executionStepModel = strings.TrimRight(code, " \t\r\n") // TODO: consider checking syntax here.
}

// RunString tests a string of Starlark code. On unexpected error, reports it,
// marks the test as failed and returns !ok. Otherwise returns ok.
func (st *ST) RunString(code string) (ok bool) {
	if code = strings.TrimRight(code, " \t\r\n"); code == "" {
		return false
	}
	assertMembers, err := starlarktest.LoadAssertModule()
	if err != nil {
		st.Errorf("internal error: %v", err)
		return false
	}
	assert, ok := assertMembers["assert"]
	if !ok {
		st.Errorf("internal error: no 'assert' defined in assert module")
		return false
	}

	st.AddValue("st", st)
	st.AddLocal("Reporter", st) // Set starlarktest reporter outside of RunThread
	st.AddValue("assert", assert)

	_, mod, err := sourceCode("startest.RunString", code, func(name string) bool {
		_, ok := st.predecls[name]
		return ok
	})
	if err != nil {
		st.Error(err)
		return false
	}

	var codeErr error
	st.RunThread(func(thread *starlark.Thread) {
		// Continue RunThread's test loop
		if codeErr != nil {
			return
		}
		_, codeErr = mod.Init(thread, st.predecls)
	})
	if codeErr != nil {
		st.Error(codeErr)
	}
	return codeErr == nil
}

// RunThread tests a function which has access to a Starlark thread.
func (st *ST) RunThread(fn func(*starlark.Thread)) {
	if !st.safetyGiven {
		st.requiredSafety = stSafe
	}

	thread := &starlark.Thread{}
	thread.RequireSafety(st.requiredSafety)
	thread.Print = func(_ *starlark.Thread, msg string) {
		st.Log(msg)
	}
	for k, v := range st.locals {
		thread.SetLocal(k, v)
	}

	resources := st.measureResources(func() {
		fn(thread)
	})
	if st.Failed() {
		return
	}

	meanMeasuredAllocs := resources.memorySum / resources.nSum
	if st.maxAllocs != math.MaxUint64 && meanMeasuredAllocs > st.maxAllocs {
		st.Errorf("measured memory is above maximum (%d > %d)", meanMeasuredAllocs, st.maxAllocs)
	}
	if st.requiredSafety.Contains(starlark.MemSafe) {
		meanDeclaredAllocs := thread.Allocs() / resources.nSum
		if meanDeclaredAllocs > st.maxAllocs {
			st.Errorf("declared allocations are above maximum (%d > %d)", meanDeclaredAllocs, st.maxAllocs)
		}
		if meanMeasuredAllocs > meanDeclaredAllocs {
			st.Errorf("measured memory is above declared allocations (%d > %d)", meanMeasuredAllocs, meanDeclaredAllocs)
		}
	}

	meanExecutionSteps := thread.ExecutionSteps() / resources.nSum
	if st.maxExecutionSteps != math.MaxUint64 && meanExecutionSteps > st.maxExecutionSteps {
		st.Errorf("declared execution steps are above maximum (%d > %d)", meanExecutionSteps, st.maxExecutionSteps)
	}
	if st.requiredSafety.Contains(starlark.CPUSafe) {
		meanModelExecutionSteps := resources.modelStepSum / resources.nSum
		if meanModelExecutionSteps > st.maxExecutionSteps {
			st.Errorf("model execution steps are above maximum (%d > %d)", meanModelExecutionSteps, st.maxExecutionSteps) // TODO: improve this lol
		}
		if meanModelExecutionSteps > meanExecutionSteps {
			st.Errorf("model execution steps are above declared execution steps (%d > %d)", meanModelExecutionSteps, meanExecutionSteps) // TODO: improve this lol
		}
	}
}

// KeepAlive causes the memory of the passed objects to be measured.
func (st *ST) KeepAlive(values ...interface{}) {
	st.alive = append(st.alive, values...)
}

type resources struct {
	memorySum    uint64
	modelStepSum uint64
	nSum         uint64
}

func (st *ST) measureResources(fn func()) resources {
	startNano := time.Now().Nanosecond()

	const nMax = 100_000
	const memoryMax = 100 * 2 << 20
	const timeMax = 1e9

	var valueTrackerOverhead uint64
	var memorySum, modelStepSum, nSum uint64
	st.N = 0

	for n := uint64(0); !st.Failed() && memorySum-valueTrackerOverhead < memoryMax && n < nMax && (time.Now().Nanosecond()-startNano) < timeMax; {
		last := n
		prevIters := uint64(st.N)
		prevMemory := memorySum
		if prevMemory <= 0 {
			prevMemory = 1
		}
		n = memoryMax * prevIters / prevMemory
		n += n / 5
		maxGrowth := last * 100
		minGrowth := last + 1
		if n > maxGrowth {
			n = maxGrowth
		} else if n < minGrowth {
			n = minGrowth
		}

		if n > nMax {
			n = nMax
		}

		st.N = int(n)
		nSum += n

		/*
			haystack := starlark.String("bbbbbbbbbbbbbbb") // len(haystack) == 15
			needle := starlark.String("s")
			st.SetMaxExecutionSteps(200)
			st.SetStepModel(`
				for _ in st.ntimes():
					"a" == "b"
			`)
			st.RunTherad(func(...) {
				string_find := starlark.String(strings.Repeat("b", st.N))
				starlark.Call(thread, string_find, Tuple{"a"}, nil)
			})

			string_find, _ := haystack.Attr("aaaa")
			st.SetMaxExecutionSteps(200)
			st.SetStepModel(`
				for _ in st.ntimes():
					for _ in range(15):
						"a" == "b"
			`)
			st.RunThread(func(thread  *starlark.Thread) {
				for i := 0; i < st.N; i++ {
					starlark.Call(thread, string_find, Tuple{needle}, nil)
				}
			})
		*/

		if st.requiredSafety.Contains(starlark.CPUSafe) && st.executionStepModel != "" {
			modelPredecls := starlark.StringDict{
				"st": st,
			}
			_, mod, err := sourceCode("startest.executionStepModel", st.executionStepModel, func(name string) bool {
				_, ok := modelPredecls[name]
				return ok
			})
			if err != nil {
				st.Error(err)
				return resources{}
			}
			executionModelThread := &starlark.Thread{}
			if _, err = mod.Init(executionModelThread, modelPredecls); err != nil { // TODO: allow global reassign (e.g. for `for` loops)
				st.Error(err)
				return resources{}
			}
			modelStepSum += executionModelThread.ExecutionSteps()
		}

		var before, after runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)

		fn()

		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&after)

		iterationMeasure := int64(after.Alloc - before.Alloc)
		valueTrackerOverhead += uint64(cap(st.alive)) * uint64(unsafe.Sizeof(interface{}(nil)))
		st.alive = nil
		if iterationMeasure > 0 {
			memorySum += uint64(iterationMeasure)
		}
	}

	if st.Failed() {
		return resources{}
	}

	if valueTrackerOverhead > memorySum {
		memorySum = 0
	} else {
		memorySum -= valueTrackerOverhead
	}

	return resources{
		memorySum:    memorySum,
		modelStepSum: modelStepSum,
		nSum:         nSum,
	}
}

func (st *ST) String() string        { return "<startest.ST>" }
func (st *ST) Type() string          { return "startest.ST" }
func (st *ST) Freeze()               { st.predecls.Freeze() }
func (st *ST) Truth() starlark.Bool  { return starlark.True }
func (st *ST) Hash() (uint32, error) { return 0, errors.New("unhashable type: startest.ST") }

var errorMethod = starlark.NewBuiltinWithSafety("error", stSafe, st_error)
var fatalMethod = starlark.NewBuiltinWithSafety("fatal", stSafe, st_fatal)
var keepAliveMethod = starlark.NewBuiltinWithSafety("keep_alive", stSafe, st_keep_alive)
var ntimesMethod = starlark.NewBuiltinWithSafety("ntimes", stSafe, st_ntimes)
var doMethod = starlark.NewBuiltinWithSafety("do", stSafe, st_do)

func (st *ST) Attr(name string) (starlark.Value, error) {
	switch name {
	case "error":
		return errorMethod.BindReceiver(st), nil
	case "fatal":
		return fatalMethod.BindReceiver(st), nil
	case "keep_alive":
		return keepAliveMethod.BindReceiver(st), nil
	case "ntimes":
		return ntimesMethod.BindReceiver(st), nil
	case "do":
		return doMethod.BindReceiver(st), nil
	case "n":
		return starlark.MakeInt(st.N), nil
	}
	return nil, nil
}

func (st *ST) AttrNames() []string {
	return []string{
		"error",
		"fatal",
		"keep_alive",
		"n",
	}
}

// st_error logs the passed Starlark objects as errors in the current test.
func st_error(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	recv := b.Receiver().(*ST)
	recv.Error(errReprs(args)...)
	return starlark.None, nil
}

// st_fatal logs the passed Starlark objects as errors in the current test
// before aborting it.
func st_fatal(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) != 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	recv := b.Receiver().(*ST)
	recv.Fatal(errReprs(args)...)
	panic(fmt.Sprintf("internal error: %T.Fatal returned", recv))
}

func errReprs(args []starlark.Value) []interface{} {
	reprs := make([]interface{}, 0, len(args))
	for _, arg := range args {
		var repr string
		if s, ok := arg.(starlark.String); ok {
			repr = s.GoString()
		} else {
			repr = arg.String()
		}
		reprs = append(reprs, repr)
	}
	return reprs
}

// st_keep_alive prevents the memory of the passed Starlark objects being
// freed. This forces the current test to measure these objects' memory.
func st_keep_alive(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	recv := b.Receiver().(*ST)
	for _, arg := range args {
		recv.KeepAlive(arg)
	}

	return starlark.None, nil
}

func st_ntimes(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s: unexpected positional arguments", b.Name())
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	recv := b.Receiver().(*ST)
	return &ntimes_iterable{recv.N}, nil
}

type ntimes_iterable struct {
	n int
}

var _ starlark.Iterable = &ntimes_iterable{}

func (it *ntimes_iterable) Freeze() {}
func (it *ntimes_iterable) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable: %s", it.Type())
}
func (it *ntimes_iterable) String() string       { return "st.ntimes()" }
func (it *ntimes_iterable) Truth() starlark.Bool { return true }
func (it *ntimes_iterable) Type() string         { return "st.ntimes" }
func (it *ntimes_iterable) Iterate() starlark.Iterator {
	return &ntimes_iterator{it.n}
}

type ntimes_iterator struct {
	n int
}

var _ starlark.SafeIterator = &ntimes_iterator{}

func (it *ntimes_iterator) Safety() starlark.Safety            { return stSafe }
func (it *ntimes_iterator) BindThread(thread *starlark.Thread) {}
func (it *ntimes_iterator) Done()                              {}
func (it *ntimes_iterator) Err() error                         { return nil }
func (it *ntimes_iterator) Next(p *starlark.Value) bool {
	if it.n > 0 {
		it.n--
		*p = starlark.None
		return true
	}
	return false
}

func st_do(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s: expected one positional argument", b.Name())
	}
	if len(kwargs) > 0 {
		return nil, fmt.Errorf("%s: unexpected keyword arguments", b.Name())
	}

	return starlark.None, nil
}
