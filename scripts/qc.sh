#! /usr/bin/env bash

if grep '"path"' cache/*.go *.go cmd/gorun/*.go
then
	echo 'bad import: "path" ' >&2
	exit 7
fi
