#!/bin/bash

pkg=github.com/calmh/ircbridged
bin=ircbridged
buildstamp=$(date +%s)
buildver=$(git describe --always)
builduser="$(whoami)@$(hostname)"
ldflags="-w -X main.buildStamp '$buildstamp' -X main.buildVersion '$buildver' -X main.buildUser '$builduser'"

export GOBIN=$(pwd)/bin
rm -rf bin

if [[ $1 == "all" ]] ; then
	pak get

	go test ./... || exit 1

	source /usr/local/golang-crosscompile/crosscompile.bash
	for arch in linux-386 linux-amd64 darwin-amd64 ; do
		echo "$arch"
		"go-$arch" install -ldflags "$ldflags" "$pkg"
		[ -f bin/"$bin" ] && mv bin/"$bin" "bin/$bin-$arch"
		[ -f bin/*/"$bin" ] && mv bin/*/"$bin" "bin/$bin-$arch"
	done
else
	go install -ldflags "$ldflags" "$pkg"
fi

rmdir -f bin/*

