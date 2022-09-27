package compile

import (
	"github.com/canonical/starlark/syntax"
)

type BytecodeBuilder struct {
	fcomp
}

func NewBytecodeBuilder() *BytecodeBuilder {
	fileName := "emitted-bytecode"

	return &BytecodeBuilder{
		fcomp{
			pcomp: &pcomp{
				prog:      &Program{},
				names:     make(map[string]uint32),
				constants: make(map[interface{}]uint32),
				functions: make(map[*Funcode]uint32),
			},
			pos: syntax.MakePosition(&fileName, 0, 0),
		},
	}
}

// func (b *BytecodeBuilder) Emit(op Opcode) {
// 	b.emit(op)
// }

// func (b *BytecodeBuilder) Emit1(op Opcode, arg uint32) {
// 	b.emit1(op, arg)
// }

func (b *BytecodeBuilder) Nop() {
	b.emit(NONE)
}

// func (b *BytecodeBuilder) PushGlobal(val starlark.Value) int {
// 	b.pcomp.prog.Globals = append(b.pcomp.prog.Globals, val)
// 	return len(b.pcomp.prog.Globals)
// }

func (b *BytecodeBuilder) PushConstant(val interface{}) uint32 {
	idx := b.pcomp.constantIndex(val)
	b.emit1(CONSTANT, idx)
	return idx
}

func (b *BytecodeBuilder) Dup() {
	b.emit(DUP)
}
