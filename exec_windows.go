package gorun

import (
	"os"
	"os/exec"
)

func sysExec(exefile string, args []string) error {
	// no exec on windows
	cmd := exec.Command(exefile, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	// try to simulate exec on windows...
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)

}
