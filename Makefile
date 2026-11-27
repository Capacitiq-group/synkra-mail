# Synkra Mail — workspace make targets (build register 3.4).
# The sandbox toolchain lives under ../tooling; on a normal dev box `go` is on PATH.
TOOLING ?= ../tooling
GO      ?= $(TOOLING)/go/bin/go
GOFLAGS ?=

.PHONY: help toolchain pg pg-stop check fmt build vet test e2e docker-build backup-verify

help:
	@echo "toolchain     restore Go + embedded Postgres after a sandbox recycle"
	@echo "pg            start the embedded Postgres (port 5433)"
	@echo "pg-stop       stop it"
	@echo "check         gofmt + build + vet + test -count=1 (what CI runs)"
	@echo "fmt           gofmt the tree"
	@echo "test          go test -count=1 ./... (never the cache — see register 3.2)"
	@echo "e2e           only the end-to-end suite"
	@echo "docker-build  build the role-selected image (register 3.3)"
	@echo "backup-verify dump the dev database, restore it into a scratch DB, verify counts"

toolchain:
	bash ../scripts/setup_toolchain.sh

pg:
	bash ../scripts/pgctl.sh start

pg-stop:
	bash ../scripts/pgctl.sh stop

fmt:
	$(GO) fmt ./...

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test:
	SYNKRA_MAIL_TEST_URL="postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable" $(GO) test -count=1 ./...

check: fmt build vet test

e2e:
	SYNKRA_MAIL_TEST_URL="postgres://synkra@127.0.0.1:5433/synkra_mail_test?sslmode=disable" $(GO) test -count=1 ./internal/e2e/

docker-build:
	docker build -t synkra-mail .

backup-verify:
	bash scripts/backup_verify.sh
