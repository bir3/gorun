package run2

import (
	"os"
	"os/exec"
)

func sysExec(exefile string, args []string) error {
	cmd := exec.Command(exefile, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	return err

}
