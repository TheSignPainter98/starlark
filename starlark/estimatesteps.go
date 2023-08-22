package starlark

import (
	"fmt"

	"github.com/canonical/starlark/internal/compile"
)

const CallSteps = 1 // CALL
const IgnoreResultSteps = 1
const LoopIterStepOverhead = 6

// EstimateSteps estimates the number of steps required to execute every line
// in the given chunk of Starlark code.
func EstimateSteps(code string) (uint64, error) {
	_, mod, err := SourceProgram("starlark.EstimateStepsModel", code, func(name string) bool {
		// Don't check for predecls as this Starlark code will not be executed.
		return true
	})
	if err != nil {
		return 0, fmt.Errorf("internal error: failed to parse:\n%s", code)
	}

	var steps uint64
	byteCode := mod.compiled.Toplevel.Code
	pc := 0
	for pc < len(byteCode) {
		op := compile.Opcode(byteCode[pc])
		fmt.Printf("\t%d:\t%s,\n", pc, op)
		steps++
		pc++
		if op >= compile.OpcodeArgMin {
		arg:
			for {
				b := byteCode[pc]
				pc++
				if b < 0x80 {
					break arg
				}
			}
		}
	}

	// Overhead of calling code within a snippet. This is
	// the cost of ignoring the result:
	// - POP the result
	// - Push NONE
	// - RETURN
	const snippetOverhead = 3
	return steps - snippetOverhead, nil
}

// MustEstimateSteps estimates the number of steps required to execute every line in
// the given chunk of Starlark code. Panics
func MustEstimateSteps(code string) uint64 {
	steps, err := EstimateSteps(code)
	if err != nil {
		panic(err)
	}
	return steps
}

// EstimateIterSteps estimates the number of steps required to execute every line
// in the given chunk of Starlark code inside a loop which runs n times.
func EstimateIterSteps(code string, n int) (uint64, error) {
	stepsPerIter, err := EstimateSteps(code)
	if err != nil {
		return 0, err
	}

	const iterOverhead = 6
	return uint64(n) * (stepsPerIter + iterOverhead), nil
}

func MustEstimateIterSteps(code string, n int) uint64 {
	steps, err := EstimateIterSteps(code, n)
	if err != nil {
		panic(err)
	}
	return steps
}
