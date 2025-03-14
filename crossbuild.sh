#!/bin/sh

# instruct gox to build statically linked binaries
export CGO_ENABLED=0
export GOTOOLCHAIN=local

mkdir binaries
gox -os="windows darwin linux" -arch="amd64 386 arm arm64" -osarch="!darwin/arm !darwin/386" -output "binaries/{{.Dir}}-{{.OS}}-{{.Arch}}"
cp config.yaml binaries/