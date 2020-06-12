package main

import (
	"go/ast"
	"os"
	"tav/src"
)

const (
	// build and emit executable
	BUILD = 0x0
)


func main() {
	src.Log("tav v_a_0_1")
	args := os.Args[1:]

	program := "X : fun(){\n}"

	if args[0] == "build" {
		src.AheadCompile(&program)
	} else if args[0] == "run" {
		src.JITCompile(&program)
	}


	a := src.StructAST{}

}
