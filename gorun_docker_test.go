package gorun_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bir3/gorun"
)

func getDockerfile(t *testing.T, dist string) (string, string) {

	var s string
	url := "https://github.com/bir3/gorun/releases/download/v$VERSION/gorun.linux-arm64"
	url = strings.ReplaceAll(url, "$VERSION", gorun.GorunVersion())
	m := make(map[string]string)
	m["alpine"] = `
FROM alpine

RUN wget $URL && mv gorun.linux-arm64 gorun && chmod 755 gorun
`

	m["ubuntu"] = `
FROM ubuntu:23.04

RUN apt-get update && apt-get install -y wget
RUN wget $URL && mv gorun.linux-arm64 gorun && chmod 755 gorun
`

	s = strings.ReplaceAll(m[dist], "$URL", url)

	s += `
ENV PATH=/:$PATH

RUN cat <<END >simple.gorun 
#! /usr/bin/env gorun

package main

import "fmt"

func main() {
	fmt.Println("standalone go code - no toolchain to install")
}
END

RUN chmod 755 simple.gorun	
`

	return url, s
}

func tempDir(t *testing.T) string {
	dir := os.Getenv("GORUN_TESTDIR")
	if dir != "" {
		dir = filepath.Join(dir, t.Name())
		err := os.MkdirAll(dir, 0777)
		if err != nil {
			t.Fatalf("%s", err)
		}
		return dir
	}
	return t.TempDir()
}

func testDist(t *testing.T, dist string) {

	if testing.Short() {
		t.Skip() // go test -short
	}
	dir := tempDir(t)
	url, s := getDockerfile(t, dist)

	help := "# run with go test -short to skip this test"
	help += "\n" + "# run with GORUN_TESTDIR=<folder> to inspect Dockerfile"
	help += "\n" + "# url: " + url

	f := filepath.Join(dir, "Dockerfile")
	err := os.WriteFile(f, []byte(s), 0666)
	if err != nil {
		t.Fatalf("create file %s failed - %s", f, err)
	}
	tag := "test/gorun"

	// build
	cmd := exec.Command("docker", "build", "-f", f, "--tag", tag, ".")

	cmd.Dir = dir
	buf, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n"+"%s\n"+"cmd failed - %s\n"+"%s", help, cmd.String(), err, string(buf))
	}

	// run
	cmd = exec.Command("docker", "run", "--rm", "--tty", tag, "simple.gorun")
	buf, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s\n"+"cmd %s\n"+"%s\n"+"%s", help, cmd.String(), err, string(buf))
	}

}

func TestAlpineDocker(t *testing.T) {
	testDist(t, "alpine")
}
func TestUbuntuDocker(t *testing.T) {
	testDist(t, "ubuntu")
}
