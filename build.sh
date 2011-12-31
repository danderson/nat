#!/bin/bash

ROOT=code.google.com/p/gonat

TARGETS="$ROOT/nat $ROOT/nat/stun $ROOT/nat/stun/stunclient $ROOT/nat/test"

mkdir -p src/code.google.com/p/gonat
rm src/code.google.com/p/gonat/nat
ln -sf `pwd`/nat src/code.google.com/p/gonat/nat

export GOPATH=`pwd`
go fix $TARGETS
go fmt $TARGETS
go install -x $TARGETS test
