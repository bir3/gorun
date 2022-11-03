
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

todo: link to executables



# Features

- Runs without installing the Go-toolchain as it's already embedded
in the executable.  
- Resulting executables are cached for fast startup.

