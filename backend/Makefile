SHELL := /bin/bash
export PATH := /opt/homebrew/bin:/opt/homebrew/opt/postgresql@17/bin:$(PATH)
-include .env
export

.PHONY: run seed seedprevisit reset migrate loadtest test build

migrate:
	go run github.com/riverqueue/river/cmd/river@latest migrate-up --database-url "$(DATABASE_URL)"
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/001_domain_schema.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/002_adjustments.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/003_cost_notices.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f migrations/004_reminders.sql

seed:
	go run ./cmd/seed

seedprevisit:
	go run ./cmd/seedprevisit -day $(or $(DAY),tomorrow)

reset:
	go run ./cmd/reset

run:
	go run ./cmd/server

build:
	mkdir -p bin && go build -o bin/coveragecheck ./cmd/server

loadtest:
	go run ./cmd/loadtest -n $(or $(N),10000)

test:
	go test ./...
