#! /usr/bin/env python3

import subprocess
import os
import sys


#
# podman run -v $(pwd):/tmp2 -it docker.io/arm64v8/alpine:3.16
# podman run -v $(pwd):/tmp2 -it arm64-ubuntu


def cmd_output(cmd):
    res = subprocess.run(cmd.split(), check=True, capture_output=True, text=True)
    return res.stdout


host_os = cmd_output("go env GOOS").strip()
host_arch = cmd_output("go env GOARCH").strip()

if host_os != "darwin" or host_arch != "arm64":
    print("ERROR: currently only works on mac with arm64")
    sys.exit(3)


def write_file(f, s, exe=False):
    with open(f, "w") as fx:
        fx.write(s)
    if exe:
        os.chmod(f, 0o755)


images = [
    [
        "arm64-ubuntu",
        """
FROM docker.io/arm64v8/ubuntu
RUN apt-get update && \
  apt-get install -y ca-certificates && \
  update-ca-certificates
""".strip()
        + "\n",
    ]
]

platforms = [
    [
        "linux-arm64-ubuntu",
        "podman run -v $(pwd)/tmp:/tmp2  -v $HOME/sdk/setup:/sdk/setup -it arm64-ubuntu tmp2/command",
        "go1.19.3.linux-arm64.tar.gz",
        """
#! /bin/sh
#set -x
cd
tar xf $sdk/setup/$gotarfile
PATH=~/go/bin:$PATH
CGO_ENABLED=0 go install github.com/bir3/gorun@v0.1.4
cp2 ()
{
    mkdir -p $2
    cp $1 $2
}
cp2 ~/go/bin/gorun /tmp2/runs-on-linux-arm64/built-by-$platform

GOOS=darwin GOARCH=arm64 go install github.com/bir3/gorun@v0.1.4
cp2 ~/go/bin/darwin_arm64/gorun /tmp2/runs-on-darwin-arm64/built-by-$platform/

""".strip()
        + "\n",
    ]
]


def build_vm_images():
    for x in images:
        tag, dockerfile_str = x
        print(tag, dockerfile_str)
        dockerfile = f"tmp/dockerfile-{tag}"
        write_file(dockerfile, dockerfile_str)
        subprocess.run(f"podman build -f {dockerfile} --tag {tag}".split(), check=True)

    print("*" * 40)


def build_gorun():
    print("*" * 80)
    for x in platforms:
        platform, vmcmd, gotarfile, command = x
        print(platform)
        vmcmd = vmcmd.replace("$(pwd)", os.getcwd())
        vmcmd = vmcmd.replace("$HOME", os.environ["HOME"])

        print(vmcmd)

        command = command.replace("$sdk", "/sdk")
        command = command.replace("$gotarfile", gotarfile)
        command = command.replace("$platform", platform)
        with open("tmp/command", "w") as fx:
            print(command, file=fx)
        os.chmod("tmp/command", 0o755)

        subprocess.run(vmcmd.split(), check=True)


def writefile2(x):
    filename, s = x
    s = s.replace("$(pwd)", os.getcwd())
    write_file(filename, s, exe=True)


test_script = [
    "tmp/test-command",
    """
#! /bin/sh
#set -x
if [ $2 != "darwin-arm64" ]
then
    podman run -v $(pwd)/tmp:/tmp2 -it $1 tmp2/test-command2 $1 $2 $3 /tmp2 $4
else
    # no vm, runs on host
    # BUG: no option/env to change gorun cache-dir
    export HOME=$(pwd)/tmp/gorun-cache
    mkdir -p $HOME/Library/Caches
    export GOCACHE=$(pwd)/tmp/gocache
    export GOMODCACHE=$(pwd)/tmp/gomodcache
    tmp/test-command2 $1 $2 $3 ./tmp $4
fi

""".strip()
    + "\n",
]

test_script2 = [
    "tmp/test-command2",
    """
#! /bin/sh
set -eu
#set -x
tmp=$4
phase=$5
if [ $phase -eq 2 ]
then
    cp $tmp/runs-on-$2/built-by-$3/goscript .
    chmod 755 goscript
    ./goscript
    exit 0
fi

cp $tmp/runs-on-$2/built-by-$3/gorun .
chmod 755 gorun
if ./gorun $tmp/goscript-blue >x 2>x2
then
    status=0
else
    status=$?
fi
cat x x2 >$tmp/x
cat x x2
grep blue x
if [ $status -eq 0 ]
then
    echo "ok : built by $3 runs on $2 in vm $1"
else
    exit $status
fi

if [ $2 != "linux-arm64" ]
then
    GOOS=linux GOARCH=arm64 ./gorun -show $tmp/goscript-green >y
    runs_on=linux-arm64
fi

if [ $2 != "darwin-arm64" ]
then
    GOOS=darwin GOARCH=arm64 ./gorun -show $tmp/goscript-green >y
    runs_on=darwin-arm64
fi

cp y $tmp
grep green y
cat y
exe=$(dirname $(cat y|grep -- '->' |cut -d ' ' -f 3))/main
cp $exe $tmp/runs-on-${runs_on}/built-by-$3/goscript

""".strip()
    + "\n",
]


def goscript(color):
    return [
        f"tmp/goscript-{color}",
        """
#! /usr/bin/env gorun

package main

import "fmt"

func main() {
   fmt.Println("standalone go code - color")
}""".replace(
            "color", color
        ).strip()
        + "\n",
    ]


test_goscript_blue = goscript("blue")
test_goscript_green = goscript("green")

# columns: cmd vm runs-on built-by
test_spec = [
    "tmp/test-command docker.io/arm64v8/ubuntu      linux-arm64  linux-arm64-ubuntu",
    "tmp/test-command docker.io/arm64v8/alpine:3.16 linux-arm64  linux-arm64-ubuntu",
    "tmp/test-command x                             darwin-arm64 linux-arm64-ubuntu",
]


if not os.path.exists("tmp/stage1-ok"):
    # build cross-compiled executables
    os.mkdir("tmp")
    build_vm_images()
    build_gorun()
    write_file("tmp/stage1-ok", "ok")

if os.path.exists("tmp/stage1-ok"):
    # test built executables
    writefile2(test_script)
    writefile2(test_script2)
    writefile2(test_goscript_blue)
    writefile2(test_goscript_green)

    status = 0
    n = 0
    for phase, ts in enumerate([test_spec, test_spec], 1):
        # phase 1 : run built gorun and build cross-compiled targets
        # phase 2 : run targets built by gorun
        print("#" * 80)
        print(f"# test phase {phase} ...")
        for k, cmdline in enumerate(ts, 1):
            cmdline = f"{cmdline} {phase}"
            res = subprocess.run(cmdline.split(), capture_output=True, text=True)
            n = n + 1
            logfile = f"tmp/logfile-{n}"

            write_file(logfile, res.stdout + res.stderr + f"exit status {res.returncode}\n")
            if res.returncode != 0:
                print("error: ", cmdline, f"# {logfile}")
                status = res.returncode
            else:
                print("ok: ", cmdline, f"# {logfile}")

    print("note: delete local tmp folder for full testrun")
    if status != 0:
        print("*** TEST FAILED ***")
    sys.exit(status)
