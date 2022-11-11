
# gorun

Run Go snippets without installing the Go toolchain.

# Example

```go
#! /usr/bin/env gorun

package main

import "fmt"

func main() {
   fmt.Println("standalone go code - no toolchain to install")
}
```

# Install

`CGO_ENABLED=0 go install github.com/bir3/gorun@v0.1.4`

So you do need the Go toolchain to build `gorun` - but once built, it runs standalone, e.g. on vanilla alpine linux

The executable is normally found in `$HOME/go/bin` - else see [go install documentation](https://pkg.go.dev/cmd/go#hdr-Compile_and_install_packages_and_dependencies)

# Features

- Runs without installing the Go toolchain as it's already embedded
in the executable.  
- Resulting executables are cached for fast startup.

# Limitations

- no Windows support yet
- no cgo support

