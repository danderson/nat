#!/bin/bash

mkdir -p src/gonat.googlecode.com/hg
rm src/gonat.googlecode.com/hg/nat
ln -sf `pwd`/nat src/gonat.googlecode.com/hg
find . -name '*.go' | xargs -n1 gofmt -w
GOPATH=`pwd` goinstall -nuke \
    gonat.googlecode.com/hg/nat/stun \
    gonat.googlecode.com/hg/nat/stun/stunclient \
    test
