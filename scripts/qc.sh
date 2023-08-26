#! /usr/bin/env bash

# "path" is not crossplatform
# => check if we accidentially imported "path" ?

if grep '"path"' cache/*.go *.go cmd/gorun/*.go
then
	echo 'bad import: "path" ' >&2
	exit 7
fi
