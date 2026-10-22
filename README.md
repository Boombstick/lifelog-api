# LifeLog API

Бэкенд личного дневника: фильмы и сериалы, поездки с тратами, календарь, заметки и push-уведомления. Живая версия: [lifel0g.ru](https://lifel0g.ru). В профиле есть кнопка «Демо-доступ».

## Стек

Go 1.26 · gin · GORM + gormigrate · PostgreSQL · Elasticsearch 8 · JWT RS256 · argon2id · Prometheus · Swagger · Docker

## Запуск

Нужны Go 1.26, PostgreSQL и Elasticsearch 8.

```bash
cp config.cfg.example config.cfg      # указать адреса, логин и пароль Postgres
# создать базы: lifelog_films, lifelog_auth, lifelog, lifelog_calendar, lifelog_notes, lifelog_notify

go run ./cmd/auth        # :9092, ключ подписи создастся в ./secrets
go run ./cmd/api         # :9091, каталог и поиск
go run ./cmd/trips       # :8080
go run ./cmd/calendar    # :9093
go run ./cmd/notes       # :9094
go run ./cmd/notify      # :9095

go run ./cmd/importer --from 2024-01-01 --to 2024-12-31 --min-votes 200   # нужен TMDB_TOKEN
go run ./cmd/indexer --recreate

go test ./...
```

Схемы баз создаются миграциями при старте. Swagger каталога: `/swagger/index.html`. Docker-образ — `Dockerfile`, раскладка сервисов — `deploy/films.yml`.

Данные о фильмах — [TMDB](https://www.themoviedb.org), города — [GeoNames](https://www.geonames.org) (CC BY 4.0).
