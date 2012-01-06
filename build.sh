#!/bin/bash

actual=$(readlink -f $0)
expected="$GOPATH/src/code.google.com/p/gonat/build.sh"

if [[ $actual != $expected ]]; then
    echo "Not in a dev setup, abort."
    exit 1
fi

ROOT=code.google.com/p/gonat
TARGETS="$ROOT $ROOT/test"

go fix $TARGETS
go fmt $TARGETS
go install -x $TARGETS
