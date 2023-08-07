package main

import (
	"fmt"
	"os"
	"syscall"
)

func sysExec(exefile string, args []string) error {
	args2 := []string{exefile}
	args2 = append(args2, args...)
	err := syscall.Exec(exefile, args2, os.Environ())
	if err != nil {
		return fmt.Errorf("syscall.Exec failed for %s - %w", exefile, err)
	}
	return nil // unreachable ! (exec should not return on success)
}
