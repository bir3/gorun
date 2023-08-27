
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

Download ready executables for linux or mac under [Releases](https://github.com/bir3/gorun/releases)

# Install from source

`go install github.com/bir3/gorun/cmd/gorun@latest`


The executable is normally found in `$HOME/go/bin` - else see [go install documentation](https://pkg.go.dev/cmd/go#hdr-Compile_and_install_packages_and_dependencies)

# Features

- Runs without installing the Go toolchain as it's already embedded
in the executable.  
- Resulting executables are cached for fast startup.
- Size of `gorun` is 44 MB and just one file


