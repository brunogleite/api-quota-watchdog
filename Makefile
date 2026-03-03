.PHONY: migrate

migrate:
	psql "$(DATABASE_URL)" -f db/migrations/001_init.sql