package repl

import (
	"bufio"
	"compiler"
	"evaluator"
	"fmt"
	"io"
	"object"
	"parser"
	"vm"
)

const PROMPT = ">>"

func StartWithInterpreter(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	env := object.NewEnvironment()
	for {
		fmt.Printf(PROMPT)
		scanned := scanner.Scan()
		if !scanned {
			return
		}

		line := scanner.Text()
		parser := parser.New(line)
		program, err := parser.ParseProgram()
		if err != nil {
			fmt.Printf("parse program failed: %s", err)
			continue
		}

		obj := evaluator.Eval(program, env)
		if evaluator.IsError(obj) {
			fmt.Printf("evaluate program failed: %s", err)
			continue
		}

		fmt.Printf("%+v\n", obj.Inspect())
	}
}

func StartWithCompiler(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)

	constants := []object.Object{}
	globalSymbalTable := compiler.NewSymbolTable()
	globals := make([]object.Object, vm.GlobalSize)

	for {
		fmt.Printf(PROMPT)
		scanned := scanner.Scan()
		if !scanned {
			return
		}

		line := scanner.Text()
		parser := parser.New(line)
		program, err := parser.ParseProgram()
		if err != nil {
			fmt.Fprintf(out, "parse program failed: %s", err)
			continue
		}

		c := compiler.NewWithStates(constants, globalSymbalTable)
		err = c.Compile(program)
		if err != nil {
			fmt.Fprintf(out, "compile program failed: %s", err)
			continue
		}

		vm := vm.NewWithGlobals(c.Bytecode(), globals)
		err = vm.Run()
		if err != nil {
			fmt.Fprintf(out, "vm run program failed: %s", err)
			continue
		}

		top := vm.StackLastTop()
		io.WriteString(out, top.Inspect())
		io.WriteString(out, "\n")
	}
}
