package vm

import (
	"code"
	"compiler"
	"fmt"
	"object"
)

const StackSize = 2048

type VM struct {
	instructions code.Instructions
	constants    []object.Object

	stack []object.Object
	sp    int
}

func New(bytecode *compiler.Bytecode) *VM {
	return &VM{
		instructions: bytecode.Instructions,
		constants:    bytecode.Constants,
		stack:        make([]object.Object, StackSize),
		sp:           -1,
	}
}

func (v *VM) pushStack(o object.Object) error {
	if v.sp >= len(v.stack) {
		return fmt.Errorf("Stack full")
	}

	v.sp++
	v.stack[v.sp] = o
	return nil
}

func (v *VM) popStack() object.Object {
	if v.sp < 0 {
		return nil
	}

	o := v.stack[v.sp]
	v.sp--
	return o
}

func (v *VM) StackTop() object.Object {
	if v.sp < 0 {
		return nil
	}

	return v.stack[v.sp]
}

func (v *VM) Run() error {
	for ip := 0; ip < len(v.instructions); ip++ {
		c := code.OpCode(v.instructions[ip])

		switch c {
		case code.OpConstant:
			index := code.ReadUint16(v.instructions[ip+1:])
			ip += 2

			fmt.Printf("constant index %d", index)
			err := v.pushStack(v.constants[index])
			if err != nil {
				return err
			}
		}
	}

	return nil
}