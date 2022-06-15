package starlark

import (
	"errors"
	"flag"
	"fmt"
	"reflect"
	"sync/atomic"
)

// This file defines resource monitors

const (
	UINTPTRS_PER_UNIT = 4
)

var (
	UNIT_SIZE = reflect.TypeOf(uintptr(0)).Size() * UINTPTRS_PER_UNIT
)

var DefaultLocationsCap = flag.Uint64("memcap", 1<<15-1, "set max usable `locations`")

type Monitor struct {
	// steps counts the number of execution steps taken within the Starlark program
	steps, maxSteps uint64

	// locationsUsed counts the abstract memory units claimed by this resource pool
	locationsUsed, locationsCap uintptr

	// Cache latest error
	err error
}

// type Sized interface {
//	// Size returns a count of abstract memory units in use by this resource.
//	// It may be used as a measure of the approximate memory usage of a
//	// Starlark application by computing the difference in its value before and
//	// after a computation.
//	//
//	// The precise meaning of a "memory unit" is not specified and may change.
//	Size() uintptr
//}

//type ResourceConsumer interface {
//	Sized
//	Monitor() *Monitor
//	SetMonitor(*Monitor)
//}

//var (
//	_ Sized = (*String)(nil)
//	_ Sized = (*Function)(nil)
//	// _ Sized = (*StringDict)(nil)
//)

//var (
//	_ ResourceConsumer = (*Dict)(nil)
//	_ ResourceConsumer = (*List)(nil)
//	_ ResourceConsumer = (*Set)(nil)
//	_ ResourceConsumer = (*rangeValue)(nil)
//	_ ResourceConsumer = (*Tuple)(nil)
//	_ ResourceConsumer = (*Int)(nil)
//)

func (mon *Monitor) initMonitor() {
	if mon.locationsCap == 0 {
		if dflt := uintptr(*DefaultLocationsCap); dflt != 0 {
			mon.locationsCap = dflt
		} else {
			mon.locationsCap-- // MaxUintptr
		}
	}

	if mon.maxSteps == 0 {
		mon.maxSteps-- // (MaxUint64)
	}
}

func (mon *Monitor) CheckUsage() error {
	if mon.err != nil {
		return mon.err
	}
	if mon.steps >= mon.maxSteps {
		mon.err = errors.New("too many steps")
		return mon.err
	}
	return nil
}

// ExecutionSteps returns a count of abstract computation steps executed
// by this thread. It is incremented by the interpreter. It may be used
// as a measure of the approximate cost of Starlark execution, by
// computing the difference in its value before and after a computation.
//
// The precise meaning of "step" is not specified and may change.
func (mon *Monitor) ExecutionSteps() uint64 {
	return mon.steps
}

// SetMaxExecutionSteps sets a limit on the number of Starlark
// computation steps that may be executed by this thread. If the
// thread's step counter exceeds this limit, the interpreter calls
// thread.Cancel("too many steps").
func (mon *Monitor) SetMaxExecutionSteps(max uint64) error {
	if mon.InUse() {
		return errors.New("cannot change execution steps of a monitor already in use")
	}
	mon.maxSteps = max
	return nil
}

func (mon *Monitor) countStep() {
	mon.steps++
}

func (mon *Monitor) LocationsUsed() uintptr {
	return mon.locationsUsed
}

func (mon *Monitor) SetLocationsCap(max uintptr) error {
	if mon.InUse() {
		return errors.New("cannot change memory cap of a monitor already in use")
	}
	mon.locationsCap = max
	return nil
}

func (mon *Monitor) InUse() bool {
	return mon.steps > 0
}

func (mon *Monitor) DeclareSizeIncrease(delta uintptr) error {
	if mon.err != nil {
		return mon.err
	}
	atomic.AddUintptr(&mon.locationsUsed, delta)
	if mon.locationsUsed >= mon.locationsCap {
		mon.err = fmt.Errorf("too much memory, failed to allocate %d extra locs", delta)
		return mon.err
	}
	return nil
}

func (mon *Monitor) DeclareSizeDecrease(delta uintptr) {
	if mon.err != nil {
		return
	}
	atomic.AddUintptr(&mon.locationsUsed, -delta)
}

//func SizeOf(obj interface{}) (size uintptr) {
//	if o, ok := obj.(Sized); ok {
//		if osize := o.Size(); osize >= 0 {
//			size += FootPrint(o, false) + osize
//		}
//	} else {
//		size += EstimateSizeOf(obj)
//	}
//	return
//}

//func EstimateSizeOf(v interface{}) uintptr {
//	// TODO(kcza): possible to be smarter with reflections?
//	// Tags could be useful here to mark which values are (not) to be considered
//	// Can skip fields whose values implement Value
//	// Handle common cases: strings, pointers, slices/arrays
//	if t, ok := v.(reflect.Type); ok {
//		return t.Size()
//	}
//	return FootPrint(v, true)
//}

//// Compute the memory footprint of the top level of a given value. All returned
//// values v are rounded: v = math.Ceil(v / UNIT_SIZE). If `resolve_indirection` is true and a pointer values has been passed
////
//// - The footprint of arrays, maps and strings are proportional to their length.
//// - The footprint of channels and slices are proportional to their capacity.
//// - The footprint of values with invalid kind is a single unit
//// - The footprint of all other values proportional to their size in bytes
//func FootPrint(v interface{}, resolve_indirection bool) uintptr {
//	vval := reflect.ValueOf(v)
//	if !vval.IsValid() {
//		return 1
//	}
//	if resolve_indirection {
//		vval = reflect.Indirect(vval)
//	}

//	vtype := vval.Type()
//	if vtype == nil {
//		return 1
//	}
//	vkind := vtype.Kind()

//	var rawFootprint uintptr
//	footprintFuncs := []func(reflect.Value, reflect.Type) uintptr{
//		reflect.Invalid:       nil,
//		reflect.Bool:          footprintSingleBlock,
//		reflect.Int:           footprintSingleBlock,
//		reflect.Int8:          footprintSingleBlock,
//		reflect.Int16:         footprintSingleBlock,
//		reflect.Int32:         footprintSingleBlock,
//		reflect.Int64:         footprintSingleBlock,
//		reflect.Uint:          footprintSingleBlock,
//		reflect.Uint8:         footprintSingleBlock,
//		reflect.Uint16:        footprintSingleBlock,
//		reflect.Uint32:        footprintSingleBlock,
//		reflect.Uint64:        footprintSingleBlock,
//		reflect.Uintptr:       footprintSingleBlock,
//		reflect.Float32:       footprintSingleBlock,
//		reflect.Float64:       footprintSingleBlock,
//		reflect.Complex64:     footprintSingleBlock,
//		reflect.Complex128:    footprintSingleBlock,
//		reflect.UnsafePointer: footprintSingleBlock,
//		reflect.Pointer:       footprintSingleBlock,
//		reflect.Array:         footprintSingleBlock,
//		reflect.Chan:          footprintChanOrSlice,
//		reflect.Slice:         footprintChanOrSlice,
//		reflect.Map:           footprintMap,
//		reflect.String:        footprintString,
//		reflect.Struct:        footprintSingleBlock,
//		reflect.Func:          footprintSingleBlock,
//		reflect.Interface:     footprintSingleBlock,
//	} // TODO: what to do if this changes in a future version?
//	rawFootprint = footprintFuncs[vkind](vval, vtype)

//	return BytesToSizeUnits(rawFootprint)
//}

//func footprintSingleBlock(_ reflect.Value, t reflect.Type) uintptr {
//	return t.Size()
//}

//func footprintChanOrSlice(v reflect.Value, t reflect.Type) uintptr {
//	return t.Size() + uintptr(v.Cap())*UNIT_SIZE
//}

//func footprintMap(v reflect.Value, t reflect.Type) uintptr {
//	return t.Size() + uintptr(v.Len())*UNIT_SIZE
//}

//func footprintString(v reflect.Value, t reflect.Type) uintptr {
//	return t.Size() + uintptr(v.Len())*reflect.TypeOf(rune(0)).Size()
//}

//func BytesToSizeUnits(raw uintptr) (size uintptr) {
//	size = raw / UNIT_SIZE
//	if raw%UNIT_SIZE != 0 {
//		size++
//	}
//	return
//}

//func SizeOfStringWithLength(len uintptr) uintptr {
//	t := reflect.TypeOf("")
//	return (t.Size() + len*reflect.TypeOf(rune(0)).Size()) / UNIT_SIZE
//}

//// Standard Sized implementations
//func (i *big.Int) Size() uintptr { return BytesToSizeUnits(uintptr(len(i.Bits()) / 8)) }
