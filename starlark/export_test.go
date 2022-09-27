package starlark

func ThreadSafety(thread *Thread) Safety {
	return thread.requiredSafety
}

const Safe = safetyFlagsLimit - 1

const SafetyFlagsLimit = safetyFlagsLimit

// func ExecOpcodes(thread *Thread, ops []compile.Opcode, globals []Value, constants []Value) (StringDict, error) {
// 	fileName := "opcode-test"

// 	opBytes := make([]byte, 0, len(ops))
// 	for _, op := range ops {
// 		opBytes = append(opBytes, byte(op))
// 	}
// 	opBytes = append(opBytes, byte(compile.NONE), byte(compile.RETURN))

// 	prog := compile.Program{}
// 	fn := Function{
// 		funcode: &compile.Funcode{
// 			Prog:     &prog,
// 			Pos:      syntax.MakePosition(&fileName, 0, 0),
// 			Name:     fileName,
// 			Code:     opBytes,
// 			Locals:   []compile.Binding{},
// 			Cells:    []int{},
// 			Freevars: []compile.Binding{},
// 			MaxStack: 2 * len(ops), // TODO(kcza): this cannot guarantee due to variable stack effects
// 		},
// 		module: &module{
// 			program:     &prog,
// 			predeclared: nil,
// 			globals:     globals,
// 			constants:   constants,
// 		},
// 	}

// 	_, err := Call(thread, &fn, nil, nil)
// 	return fn.Globals(), err
// }
