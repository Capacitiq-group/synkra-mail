#!/bin/bash
# Synkra Email build/dev environment. Source this in every shell:
#   source /home/z/my-project/synkra-email/env.sh
export GOROOT=/home/z/my-project/tooling/go
export PATH=$GOROOT/bin:$PATH
export GOPATH=/home/z/my-project/tooling/gopath
export GOMODCACHE=$GOPATH/pkg/mod
export LD_LIBRARY_PATH=/home/z/my-project/tooling/pg/node_modules/@embedded-postgres/linux-x64/native/lib
export PGBIN=/home/z/my-project/tooling/pg/node_modules/@embedded-postgres/linux-x64/native/bin
export SYNKRA_MAIL_DATABASE_URL="postgres://synkra@127.0.0.1:5433/synkra_mail?sslmode=disable"
export SYNKRA_MAIL_ENV=dev
