#!/bin/sh

mkdir binaries
gox -os="windows darwin linux" -arch="amd64 386 arm arm64" -osarch="!darwin/arm !darwin/arm64 !darwin/386" -output "binaries/{{.Dir}}_{{.OS}}_{{.Arch}}"