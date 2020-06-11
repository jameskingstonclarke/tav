package main

import (
	"fmt"
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
	if args[0] == "build" {
		build()
	}
}

func build() {
	src.AheadCompile(":=")
	fmt.Scanln()
}
