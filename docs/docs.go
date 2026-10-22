package docs

import "github.com/swaggo/swag"

const docTemplate = `{
    "schemes": {{ marshal .Schemes }},
    "swagger": "2.0",
    "info": {
        "description": "{{escape .Description}}",
        "title": "{{.Title}}",
        "contact": {},
        "version": "{{.Version}}"
    },
    "host": "{{.Host}}",
    "basePath": "{{.BasePath}}",
    "paths": {
        "/admin/films/sync": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Состояние последнего прогона в этом процессе. После перезапуска сервиса — idle.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Admin"
                ],
                "summary": "Состояние синхронизации каталога",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SyncStatusResponse"
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "403": {
                        "description": "Forbidden",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    }
                }
            },
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Импорт идёт в фоне: ручка отвечает сразу идентификатором прогона, ход смотреть через GET той же ручки. Пока прогон идёт, второй запуск отбивается 409. Переиндексация Elasticsearch в импорт не входит — её делает cmd/indexer.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Admin"
                ],
                "summary": "Запустить синхронизацию каталога с TMDB",
                "parameters": [
                    {
                        "description": "Границы по году выпуска",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.StartSyncRequest"
                        }
                    }
                ],
                "responses": {
                    "202": {
                        "description": "Accepted",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SyncStatusResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "403": {
                        "description": "Forbidden",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    },
                    "409": {
                        "description": "Conflict",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SyncStatusResponse"
                        }
                    },
                    "503": {
                        "description": "Service Unavailable",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    }
                }
            }
        },
        "/films": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Тот же механизм, что и у поиска, только без строки запроса.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Каталог фильмов с фильтрами и пагинацией",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Слаги жанров через запятую, между ними ИЛИ",
                        "name": "genre",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Год выпуска, от",
                        "name": "year_from",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Год выпуска, до",
                        "name": "year_to",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Длительность в минутах, от",
                        "name": "runtime_from",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Длительность в минутах, до",
                        "name": "runtime_to",
                        "in": "query"
                    },
                    {
                        "type": "number",
                        "description": "Рейтинг TMDB, от",
                        "name": "rating_from",
                        "in": "query"
                    },
                    {
                        "type": "number",
                        "description": "Рейтинг LifeLog, от",
                        "name": "local_from",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "popularity, tmdb_rating, local_rating, year, runtime, title",
                        "name": "sort",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "asc или desc; по умолчанию asc у title, desc у остального",
                        "name": "order",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    },
                    {
                        "type": "boolean",
                        "description": "Убрать фильмы с отметкой «просмотрено». Требует токена",
                        "name": "hide_watched",
                        "in": "query"
                    },
                    {
                        "type": "boolean",
                        "description": "Вернуть количество фильмов по жанрам",
                        "name": "facets",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_FilmCatalogItem"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "502": {
                        "description": "Bad Gateway"
                    }
                }
            }
        },
        "/films/filters": {
            "get": {
                "description": "Крайние год и длительность среди того, что лежит в каталоге.\nПанель фильтров строится по ним, а не по зашитым числам: после\nкаждого импорта каталог растёт, и зашитая нижняя граница молча\nпрячет всё, что старше.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Границы каталога для панели фильтров",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.FilmBoundsResponse"
                        }
                    },
                    "502": {
                        "description": "Bad Gateway"
                    }
                }
            }
        },
        "/films/genres": {
            "get": {
                "description": "Слаг — ключ фильтра, названия — то, что видит человек.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Справочник жанров",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "array",
                            "items": {
                                "$ref": "#/definitions/lifelog-api_internal_service.GenreResponse"
                            }
                        }
                    }
                }
            }
        },
        "/films/my": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Наборы пересекаются: просмотренный и оценённый фильм остаётся в\nсписке к просмотру, пока отметку не сняли. Источник — Postgres,\nа не каталог в Elasticsearch, поэтому фильтров по жанрам и году\nздесь нет.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Свои фильмы: список к просмотру, просмотренные, оценённые",
                "parameters": [
                    {
                        "type": "string",
                        "description": "watchlist (по умолчанию), watched, rated",
                        "name": "set",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "added, watched, rate, title. По умолчанию — свой для каждого набора. rate доступен только в rated, watched — в watched и rated",
                        "name": "sort",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.MyFilmsPage"
                        }
                    },
                    "400": {
                        "description": "Bad Request",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            }
        },
        "/films/my/counts": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Те же наборы, что у /films/my?set=, посчитанные одним запросом:\nкаждое число равно total соответствующей страницы. Наборы\nпересекаются, поэтому сумма не равна числу отметок.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Сколько фильмов в каждом своём наборе",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.MyFilmCounts"
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            }
        },
        "/films/recommendations": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Подборка по вашим оценкам, без уже просмотренного. Пустой список —\nштатный ответ для того, кто ещё ничего не оценил.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Вам понравится",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Сколько вернуть, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.RecommendedFilms"
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "502": {
                        "description": "Bad Gateway"
                    }
                }
            }
        },
        "/films/{id}": {
            "get": {
                "description": "Without a token, catalog fields only. With a token, adds the viewer own entry (my_entry) and reviews by others that subscription rules allow (reviews).",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Получить фильм",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Film Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.FilmCard"
                        }
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/films/{id}/dropped": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Series"
                ],
                "summary": "Mark a series as dropped",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Series id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Series"
                ],
                "summary": "Remove the dropped mark",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Series id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    }
                }
            }
        },
        "/films/{id}/entry": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Отметка текущего пользователя: список «посмотреть», просмотр, оценка, отзыв.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Своя отметка о фильме",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор фильма",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.FilmEntryResponse"
                        }
                    },
                    "404": {
                        "description": "Not Found",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    }
                }
            }
        },
        "/films/{id}/review": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Оценка сама ставит просмотр, если его не было, и убирает фильм из списка к просмотру. Пустой текст без оценки только стирает прежний текст и ничего не отмечает.",
                "consumes": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Оценить фильм и/или оставить отзыв",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Film Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Оценка и отзыв",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CreateReviewRequest"
                        }
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Снять оценку и отзыв",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор фильма",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    }
                }
            }
        },
        "/films/{id}/seasons/watched": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Sets every aired regular season, or those up to up_to, to watched; season ratings stay.",
                "tags": [
                    "Series"
                ],
                "summary": "Mark all aired seasons watched",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Series id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Optional up_to",
                        "name": "request",
                        "in": "body",
                        "schema": {
                            "$ref": "#/definitions/internal_handler.markSeasonsRequest"
                        }
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    }
                }
            }
        },
        "/films/{id}/seasons/{number}": {
            "put": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "state: watching, watched or null (removes the entry); rate 1..10 only with watched. An absent rate keeps the current one; a rate without a state key changes only the rating of a watched season. An empty body removes the entry.",
                "consumes": [
                    "application/json"
                ],
                "tags": [
                    "Series"
                ],
                "summary": "Set a season's state and rating",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Series id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "type": "integer",
                        "description": "Season number",
                        "name": "number",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "State and optional rate",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.seasonStateRequest"
                        }
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "409": {
                        "description": "Conflict"
                    }
                }
            }
        },
        "/films/{id}/similar": {
            "get": {
                "description": "source=tmdb отдаёт готовый список TMDB, source=own — наш подбор\nпо индексу. Поле source в ответе говорит, что реально отдали:\nсписка TMDB на каждый фильм нет, и тогда сервер отдаёт свой.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Похожие фильмы",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Film Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "type": "string",
                        "description": "tmdb (по умолчанию) или own",
                        "name": "source",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Сколько вернуть, по умолчанию 12, максимум 50",
                        "name": "limit",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SimilarFilms"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "502": {
                        "description": "Bad Gateway"
                    }
                }
            }
        },
        "/films/{id}/watched": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Отметить фильм просмотренным",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Film Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Пока на фильме стоит оценка, снять просмотр нельзя: сначала снимается оценка.",
                "tags": [
                    "Film"
                ],
                "summary": "Снять отметку о просмотре",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор фильма",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "409": {
                        "description": "Conflict",
                        "schema": {
                            "type": "object",
                            "additionalProperties": {
                                "type": "string"
                            }
                        }
                    }
                }
            }
        },
        "/films/{id}/watchlist": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Добавить фильм в список \"посмотреть позже\"",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Film Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "tags": [
                    "Film"
                ],
                "summary": "Убрать из списка к просмотру",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор фильма",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    }
                }
            }
        },
        "/geo/cities": {
            "get": {
                "description": "Города от 15 тысяч жителей, по убыванию населения.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Geo"
                ],
                "summary": "Справочник городов",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Код страны, ISO alpha-2",
                        "name": "country",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Начало названия",
                        "name": "q",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 50, максимум 100",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CityPage"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    }
                }
            }
        },
        "/geo/cities/nearest": {
            "get": {
                "description": "Ближайший город из справочника. Внешний геокодер не используется.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Geo"
                ],
                "summary": "Город по координатам",
                "parameters": [
                    {
                        "type": "number",
                        "description": "Широта",
                        "name": "lat",
                        "in": "query",
                        "required": true
                    },
                    {
                        "type": "number",
                        "description": "Долгота",
                        "name": "lon",
                        "in": "query",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CityResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/geo/countries": {
            "get": {
                "description": "Названия на русском и английском. Пагинации нет: стран 250.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Geo"
                ],
                "summary": "Справочник стран",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Поиск по названию или коду",
                        "name": "q",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "type": "array",
                            "items": {
                                "$ref": "#/definitions/lifelog-api_internal_service.CountryResponse"
                            }
                        }
                    }
                }
            }
        },
        "/imports": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Последние 10, новые первыми, без списка ненайденного.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Imports"
                ],
                "summary": "Мои импорты",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_imdbimport.JobsResponse"
                        }
                    }
                }
            }
        },
        "/imports/imdb": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "CSV-экспорт IMDb (Your Ratings или Watchlist) полем file, до 5 МБ и 20 000 строк. Вид списка определяется по заголовку. Импорт идёт в фоне; прогресс — GET /imports/{id}. Свои отметки пользователя не перезаписываются, оценки в TMDB не отправляются.",
                "consumes": [
                    "multipart/form-data"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Imports"
                ],
                "summary": "Запустить импорт из IMDb",
                "parameters": [
                    {
                        "type": "file",
                        "description": "CSV-экспорт IMDb",
                        "name": "file",
                        "in": "formData",
                        "required": true
                    }
                ],
                "responses": {
                    "202": {
                        "description": "Accepted",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_imdbimport.JobResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "409": {
                        "description": "Conflict"
                    },
                    "413": {
                        "description": "Request Entity Too Large"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/imports/{id}": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Прогресс и отчёт одного своего импорта; чужой и несуществующий — 404.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Imports"
                ],
                "summary": "Импорт",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "ID импорта",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_imdbimport.JobResponse"
                        }
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/me/visibility": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Sharing"
                ],
                "summary": "Кому видны мои списки",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/internal_handler.visibilityResponse"
                        }
                    }
                }
            },
            "put": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Присылаются только меняемые поля, остальные остаются как были.",
                "consumes": [
                    "application/json"
                ],
                "tags": [
                    "Sharing"
                ],
                "summary": "Изменить видимость списков",
                "parameters": [
                    {
                        "description": "Уровни: private или subscribers",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.visibilityRequest"
                        }
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "400": {
                        "description": "Bad Request"
                    }
                }
            }
        },
        "/people/{id}": {
            "get": {
                "description": "Фильмография приходит целиком, без курсора: у самого снятого\nчеловека в каталоге 34 фильма. Роли у фильма — чем человек занят\nименно в нём; роли у человека — объединение по всей фильмографии.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Person"
                ],
                "summary": "Карточка человека с фильмографией",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Person Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.PersonCard"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/places": {
            "get": {
                "description": "Без токена видны только одобренные места; автор видит и свои на модерации.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "Список мест с фильтрами и пагинацией",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Код страны, ISO alpha-2",
                        "name": "country",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Идентификатор города из справочника",
                        "name": "city_id",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Тип места: food, culture, park, stay, other",
                        "name": "kind",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Поиск по названию, адресу и категории",
                        "name": "q",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.PlacePage"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    }
                }
            },
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "Создать место",
                "parameters": [
                    {
                        "description": "Данные места",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CreatePlaceRequest"
                        }
                    }
                ],
                "responses": {
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CreatePlaceResponse"
                        }
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            }
        },
        "/places/{id}": {
            "get": {
                "description": "Без токена видны только одобренные места; автор видит и свои на модерации.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "получить место",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Place Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.PlaceResponse"
                        }
                    }
                }
            },
            "put": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Менять место может только его автор или администратор.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "изменить место",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Place Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Данные места",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.UpdatePlaceRequest"
                        }
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "403": {
                        "description": "Forbidden"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Удалить место может только его автор или администратор.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "удалить место",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Place Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "403": {
                        "description": "Forbidden"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/places/{id}/review": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Place"
                ],
                "summary": "добавить отзыв на место",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Place Id",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Отзыв, не длиннее 255 символов",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.AddReviewRequest"
                        }
                    }
                ],
                "responses": {
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CreatePlaceResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        },
        "/search": {
            "get": {
                "description": "Поле top говорит, какой блок показывать первым: films или people.\nРешает сервер: полное совпадение со строкой поиска выносит вперёд\nфильм, иначе — людей.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Search"
                ],
                "summary": "Поиск по фильмам и людям сразу",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Строка поиска",
                        "name": "q",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Сколько результатов в каждом разделе, по умолчанию 20",
                        "name": "limit",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.CombinedSearch"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    }
                }
            }
        },
        "/search/films": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Search"
                ],
                "summary": "Поиск фильмов с фильтрами",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Строка поиска; без неё это просто каталог с фильтрами",
                        "name": "q",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Где искать: all (по умолчанию), title — только по названию, cast — только по актёрам и режиссёрам",
                        "name": "scope",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Слаги жанров через запятую, между ними ИЛИ",
                        "name": "genre",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Год выпуска, от",
                        "name": "year_from",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Год выпуска, до",
                        "name": "year_to",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Длительность в минутах, от",
                        "name": "runtime_from",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Длительность в минутах, до",
                        "name": "runtime_to",
                        "in": "query"
                    },
                    {
                        "type": "number",
                        "description": "Рейтинг TMDB, от",
                        "name": "rating_from",
                        "in": "query"
                    },
                    {
                        "type": "number",
                        "description": "Рейтинг LifeLog, от",
                        "name": "local_from",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "relevance, popularity, tmdb_rating, local_rating, year, runtime, title",
                        "name": "sort",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "asc или desc; по умолчанию asc у title, desc у остального",
                        "name": "order",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    },
                    {
                        "type": "boolean",
                        "description": "Убрать фильмы с отметкой «просмотрено». Требует токена",
                        "name": "hide_watched",
                        "in": "query"
                    },
                    {
                        "type": "boolean",
                        "description": "Вернуть количество фильмов по жанрам",
                        "name": "facets",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_FilmSearchItem"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    }
                }
            }
        },
        "/search/people": {
            "get": {
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Search"
                ],
                "summary": "Поиск людей — актёров и режиссёров",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Строка поиска",
                        "name": "q",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы, по умолчанию 20, максимум 50",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы из next_cursor",
                        "name": "cursor",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_PersonSearchItem"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    }
                }
            }
        },
        "/shared-lists": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Где я создатель или участник, свежие изменения первыми.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Мои совместные списки",
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListsPage"
                        }
                    }
                }
            },
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Добавить можно только одобренного подписчика создателя, не больше 20. Подписки проверяет auth; недоступен — 503.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Создать совместный список",
                "parameters": [
                    {
                        "description": "Название и участники",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.sharedListCreateRequest"
                        }
                    }
                ],
                "responses": {
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListCard"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/shared-lists/{id}": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Карточка совместного списка",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListCard"
                        }
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            },
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Только создатель. Удаляет у всех, вместе с фильмами и розыгрышами.",
                "tags": [
                    "SharedLists"
                ],
                "summary": "Удалить совместный список",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "403": {
                        "description": "Forbidden"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            },
            "patch": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Только создатель. member_ids заменяет состав целиком; подписка проверяется только у добавляемых, auth недоступен — 503.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Изменить совместный список",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Меняемые поля",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.sharedListUpdateRequest"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListCard"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "403": {
                        "description": "Forbidden"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/shared-lists/{id}/draws": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "История розыгрышей",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "type": "integer",
                        "description": "До 50, по умолчанию 20",
                        "name": "limit",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListDrawsPage"
                        }
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            },
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "one — один фильм; top — count (3..10) разных фильмов, но не больше, чем есть в списке. count в ответе — сколько фильмов выпало.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Случайный выбор из совместного списка",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Режим",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.sharedListDrawRequest"
                        }
                    }
                ],
                "responses": {
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListDraw"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "409": {
                        "description": "Conflict"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/shared-lists/{id}/items": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Повтор того же фильма — 200 с уже существующим элементом.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "SharedLists"
                ],
                "summary": "Добавить фильм в совместный список",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "description": "Фильм",
                        "name": "request",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.sharedListItemRequest"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListItem"
                        }
                    },
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.SharedListItem"
                        }
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "409": {
                        "description": "Conflict"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/shared-lists/{id}/items/{film_id}": {
            "delete": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Может любой участник.",
                "tags": [
                    "SharedLists"
                ],
                "summary": "Убрать фильм из совместного списка",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "type": "integer",
                        "description": "Идентификатор фильма",
                        "name": "film_id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/shared-lists/{id}/leave": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Участник, не создатель: создателю — 400.",
                "tags": [
                    "SharedLists"
                ],
                "summary": "Выйти из совместного списка",
                "parameters": [
                    {
                        "type": "integer",
                        "description": "Идентификатор списка",
                        "name": "id",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "204": {
                        "description": "No Content"
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/tmdb/add": {
            "post": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Догружает карточку из TMDB без порога голосов (у сериала — с сезонами) и сразу индексирует. Уже есть в каталоге — 200 с created=false, лимит не тратится. Не больше 20 добавлений за скользящие сутки.",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "TMDB"
                ],
                "summary": "Добавить в каталог из TMDB",
                "parameters": [
                    {
                        "description": "Что добавить",
                        "name": "body",
                        "in": "body",
                        "required": true,
                        "schema": {
                            "$ref": "#/definitions/internal_handler.tmdbAddRequest"
                        }
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_tmdbadd.AddResponse"
                        }
                    },
                    "201": {
                        "description": "Created",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_tmdbadd.AddResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "404": {
                        "description": "Not Found"
                    },
                    "429": {
                        "description": "Too Many Requests"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/tmdb/search": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Фильмы и сериалы из TMDB (/search/multi, ru-RU, без взрослого), в порядке TMDB. catalog_id — id нашей карточки, если она уже в каталоге. query после обрезки 2..100 символов, page 1..20.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "TMDB"
                ],
                "summary": "Поиск в TMDB",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Строка поиска",
                        "name": "query",
                        "in": "query",
                        "required": true
                    },
                    {
                        "type": "integer",
                        "default": 1,
                        "description": "Страница, 1..20",
                        "name": "page",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_tmdbadd.SearchResponse"
                        }
                    },
                    "400": {
                        "description": "Bad Request"
                    },
                    "401": {
                        "description": "Unauthorized"
                    },
                    "503": {
                        "description": "Service Unavailable"
                    }
                }
            }
        },
        "/users/{id}/films": {
            "get": {
                "security": [
                    {
                        "BearerAuth": []
                    }
                ],
                "description": "Идентификатор — uuid учётки auth, тот же, что в подписках.",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Sharing"
                ],
                "summary": "Список фильмов другого пользователя",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Идентификатор учётки",
                        "name": "id",
                        "in": "path",
                        "required": true
                    },
                    {
                        "type": "string",
                        "description": "watchlist, watched, rated",
                        "name": "set",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "added, watched, rate, title",
                        "name": "sort",
                        "in": "query"
                    },
                    {
                        "type": "integer",
                        "description": "Размер страницы",
                        "name": "limit",
                        "in": "query"
                    },
                    {
                        "type": "string",
                        "description": "Курсор следующей страницы",
                        "name": "cursor",
                        "in": "query"
                    }
                ],
                "responses": {
                    "200": {
                        "description": "OK",
                        "schema": {
                            "$ref": "#/definitions/lifelog-api_internal_service.MyFilmsPage"
                        }
                    },
                    "403": {
                        "description": "Forbidden"
                    },
                    "404": {
                        "description": "Not Found"
                    }
                }
            }
        }
    },
    "definitions": {
        "internal_handler.markSeasonsRequest": {
            "type": "object",
            "properties": {
                "up_to": {
                    "type": "integer"
                }
            }
        },
        "internal_handler.seasonStateRequest": {
            "type": "object",
            "properties": {
                "rate": {
                    "type": "integer"
                },
                "state": {
                    "type": "string"
                }
            }
        },
        "internal_handler.sharedListCreateRequest": {
            "type": "object",
            "properties": {
                "member_ids": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "title": {
                    "type": "string"
                }
            }
        },
        "internal_handler.sharedListDrawRequest": {
            "type": "object",
            "properties": {
                "count": {
                    "type": "integer"
                },
                "mode": {
                    "type": "string"
                }
            }
        },
        "internal_handler.sharedListItemRequest": {
            "type": "object",
            "properties": {
                "film_id": {
                    "type": "integer"
                }
            }
        },
        "internal_handler.sharedListUpdateRequest": {
            "type": "object",
            "properties": {
                "member_ids": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "title": {
                    "type": "string"
                }
            }
        },
        "internal_handler.tmdbAddRequest": {
            "type": "object",
            "properties": {
                "kind": {
                    "type": "string"
                },
                "tmdb_id": {
                    "description": "A pointer tells a missing id from 0; a string or a fraction fails to\ndecode into int64 and is a 400.",
                    "type": "integer"
                }
            }
        },
        "internal_handler.visibilityRequest": {
            "type": "object",
            "properties": {
                "rated": {
                    "type": "string"
                },
                "watched": {
                    "type": "string"
                },
                "watchlist": {
                    "type": "string"
                }
            }
        },
        "internal_handler.visibilityResponse": {
            "type": "object",
            "properties": {
                "rated": {
                    "type": "string"
                },
                "watched": {
                    "type": "string"
                },
                "watchlist": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_imdbimport.JobResponse": {
            "type": "object",
            "properties": {
                "added": {
                    "type": "integer"
                },
                "catalog_added": {
                    "type": "integer"
                },
                "created_at": {
                    "type": "string"
                },
                "error": {
                    "type": "string"
                },
                "failed": {
                    "type": "integer"
                },
                "finished_at": {
                    "type": "string"
                },
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "$ref": "#/definitions/lifelog-api_internal_imdbimport.ListKind"
                },
                "not_found": {
                    "type": "integer"
                },
                "not_found_items": {
                    "description": "Pointer so the list can leave the field out while a single job always\ncarries it, as [] when nothing was missed.",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_imdbimport.NotFoundItem"
                    }
                },
                "processed": {
                    "type": "integer"
                },
                "skipped_existing": {
                    "type": "integer"
                },
                "source": {
                    "type": "string"
                },
                "status": {
                    "$ref": "#/definitions/lifelog-api_internal_imdbimport.Status"
                },
                "total": {
                    "type": "integer"
                },
                "unsupported": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_imdbimport.JobsResponse": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_imdbimport.JobResponse"
                    }
                }
            }
        },
        "lifelog-api_internal_imdbimport.ListKind": {
            "type": "string",
            "enum": [
                "ratings",
                "watchlist"
            ],
            "x-enum-varnames": [
                "ListRatings",
                "ListWatchlist"
            ]
        },
        "lifelog-api_internal_imdbimport.NotFoundItem": {
            "type": "object",
            "properties": {
                "imdb_id": {
                    "type": "string"
                },
                "title": {
                    "type": "string"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_imdbimport.Status": {
            "type": "string",
            "enum": [
                "queued",
                "running",
                "done",
                "failed"
            ],
            "x-enum-varnames": [
                "StatusQueued",
                "StatusRunning",
                "StatusDone",
                "StatusFailed"
            ]
        },
        "lifelog-api_internal_model.ModerationStatus": {
            "type": "string",
            "enum": [
                "unknown",
                "pending",
                "approved",
                "rejected"
            ],
            "x-enum-varnames": [
                "ModerationStatusUnknown",
                "ModerationStatusPending",
                "ModerationStatusApproved",
                "ModerationStatusRejected"
            ]
        },
        "lifelog-api_internal_service.AddReviewRequest": {
            "type": "object",
            "required": [
                "rate"
            ],
            "properties": {
                "description": {
                    "type": "string"
                },
                "rate": {
                    "type": "integer",
                    "maximum": 5,
                    "minimum": 1
                }
            }
        },
        "lifelog-api_internal_service.CityPage": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.CityResponse"
                    }
                },
                "next_cursor": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.CityResponse": {
            "type": "object",
            "properties": {
                "country_code": {
                    "type": "string"
                },
                "id": {
                    "type": "integer"
                },
                "lat": {
                    "type": "number"
                },
                "lon": {
                    "type": "number"
                },
                "name_en": {
                    "type": "string"
                },
                "name_ru": {
                    "type": "string"
                },
                "population": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.CombinedSearch": {
            "type": "object",
            "properties": {
                "films": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_FilmSearchItem"
                },
                "people": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_PersonSearchItem"
                },
                "top": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.CountryResponse": {
            "type": "object",
            "properties": {
                "code": {
                    "type": "string"
                },
                "name_en": {
                    "type": "string"
                },
                "name_ru": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.CreatePlaceRequest": {
            "type": "object",
            "required": [
                "address",
                "category",
                "name"
            ],
            "properties": {
                "address": {
                    "type": "string",
                    "maxLength": 128,
                    "example": "Ленина"
                },
                "category": {
                    "type": "string",
                    "maxLength": 32,
                    "example": "Кафе"
                },
                "city_id": {
                    "type": "integer"
                },
                "country_code": {
                    "description": "CountryCode и CityID берутся из справочника географии, а не вводятся\nстрокой: иначе «Таиланд», «Тайланд» и «Thailand» станут тремя странами\nи фильтр по стране начнёт врать.",
                    "type": "string",
                    "example": "TH"
                },
                "kind": {
                    "description": "Kind — тип места из фиксированного перечня: food, culture, park, stay, other.\nНеобязателен: без него место получает other.",
                    "type": "string",
                    "example": "food"
                },
                "lat": {
                    "type": "number"
                },
                "lon": {
                    "type": "number"
                },
                "name": {
                    "type": "string",
                    "maxLength": 128,
                    "example": "Alex"
                }
            }
        },
        "lifelog-api_internal_service.CreatePlaceResponse": {
            "type": "object",
            "properties": {
                "id": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.CreateReviewRequest": {
            "type": "object",
            "properties": {
                "description": {
                    "type": "string",
                    "maxLength": 255
                },
                "rate": {
                    "type": "integer",
                    "maximum": 10,
                    "minimum": 1
                }
            }
        },
        "lifelog-api_internal_service.Facet": {
            "type": "object",
            "properties": {
                "count": {
                    "type": "integer"
                },
                "key": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmBoundsResponse": {
            "type": "object",
            "properties": {
                "runtime_max": {
                    "type": "integer"
                },
                "runtime_min": {
                    "type": "integer"
                },
                "year_max": {
                    "type": "integer"
                },
                "year_min": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.FilmCard": {
            "type": "object",
            "properties": {
                "credits": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmCardCredit"
                    }
                },
                "genre_slugs": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "genres": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "imdb_id": {
                    "type": "string"
                },
                "kind": {
                    "type": "string"
                },
                "local_rating": {
                    "type": "number"
                },
                "local_votes": {
                    "type": "integer"
                },
                "my_entry": {
                    "description": "MyEntry is nil both without a token and when the viewer marked nothing:\nthe client tells those apart by whether it sent a token.",
                    "allOf": [
                        {
                            "$ref": "#/definitions/lifelog-api_internal_service.FilmCardEntry"
                        }
                    ]
                },
                "poster_url": {
                    "type": "string"
                },
                "release_date": {
                    "type": "string"
                },
                "reviews": {
                    "description": "Reviews are other people's entries, already filtered by visibility.",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmCardReview"
                    }
                },
                "runtime": {
                    "type": "integer"
                },
                "seasons": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmCardSeason"
                    }
                },
                "series": {
                    "description": "Series and Seasons are present only for kind \"series\".",
                    "allOf": [
                        {
                            "$ref": "#/definitions/lifelog-api_internal_service.FilmCardSeries"
                        }
                    ]
                },
                "synopsis": {
                    "type": "string"
                },
                "title": {
                    "type": "string"
                },
                "title_original": {
                    "type": "string"
                },
                "tmdb_id": {
                    "type": "integer"
                },
                "tmdb_rating": {
                    "type": "number"
                },
                "tmdb_votes": {
                    "type": "integer"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardCredit": {
            "type": "object",
            "properties": {
                "billing_order": {
                    "type": "integer"
                },
                "name": {
                    "type": "string"
                },
                "original_name": {
                    "type": "string"
                },
                "person_id": {
                    "type": "integer"
                },
                "photo_url": {
                    "type": "string"
                },
                "role": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardEntry": {
            "type": "object",
            "properties": {
                "current_season": {
                    "type": "integer"
                },
                "description": {
                    "type": "string"
                },
                "progress": {
                    "$ref": "#/definitions/lifelog-api_internal_service.FilmCardProgress"
                },
                "rate": {
                    "type": "integer"
                },
                "seasons": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmCardSeasonEntry"
                    }
                },
                "series_rating": {
                    "type": "number"
                },
                "series_state": {
                    "description": "Series-only tracking fields, filled by applySeriesProgress.",
                    "type": "string"
                },
                "series_watched": {
                    "type": "boolean"
                },
                "watched_at": {
                    "type": "string"
                },
                "watchlisted": {
                    "type": "boolean"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardProgress": {
            "type": "object",
            "properties": {
                "aired": {
                    "type": "integer"
                },
                "watched": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardReview": {
            "type": "object",
            "properties": {
                "description": {
                    "type": "string"
                },
                "rate": {
                    "type": "integer"
                },
                "user_name": {
                    "type": "string"
                },
                "watched_at": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardSeason": {
            "type": "object",
            "properties": {
                "air_date": {
                    "type": "string"
                },
                "aired": {
                    "description": "Aired is computed here so clients never compare clocks.",
                    "type": "boolean"
                },
                "episode_count": {
                    "type": "integer"
                },
                "id": {
                    "type": "integer"
                },
                "name": {
                    "type": "string"
                },
                "number": {
                    "type": "integer"
                },
                "poster_url": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardSeasonEntry": {
            "type": "object",
            "properties": {
                "number": {
                    "type": "integer"
                },
                "rate": {
                    "type": "integer"
                },
                "state": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmCardSeries": {
            "type": "object",
            "properties": {
                "first_air_date": {
                    "type": "string"
                },
                "in_production": {
                    "type": "boolean"
                },
                "last_air_date": {
                    "type": "string"
                },
                "networks": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "next_air_date": {
                    "type": "string"
                },
                "number_of_episodes": {
                    "type": "integer"
                },
                "number_of_seasons": {
                    "type": "integer"
                },
                "status": {
                    "type": "string"
                },
                "type": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.FilmCatalogItem": {
            "type": "object",
            "properties": {
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "local_rating": {
                    "type": "number"
                },
                "local_votes": {
                    "type": "integer"
                },
                "poster_url": {
                    "type": "string"
                },
                "title": {
                    "type": "string"
                },
                "tmdb_rating": {
                    "type": "number"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.FilmEntryResponse": {
            "type": "object",
            "properties": {
                "description": {
                    "type": "string"
                },
                "film_id": {
                    "type": "integer"
                },
                "rate": {
                    "type": "integer"
                },
                "watched_at": {
                    "type": "string"
                },
                "watchlisted": {
                    "type": "boolean"
                }
            }
        },
        "lifelog-api_internal_service.FilmSearchItem": {
            "type": "object",
            "properties": {
                "directors": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "genres": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "local_rating": {
                    "type": "number"
                },
                "local_votes": {
                    "type": "integer"
                },
                "poster_url": {
                    "type": "string"
                },
                "runtime": {
                    "type": "integer"
                },
                "title": {
                    "type": "string"
                },
                "tmdb_rating": {
                    "type": "number"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.FilmographyItem": {
            "type": "object",
            "properties": {
                "genres": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "local_rating": {
                    "type": "number"
                },
                "local_votes": {
                    "type": "integer"
                },
                "poster_url": {
                    "type": "string"
                },
                "roles": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "runtime": {
                    "type": "integer"
                },
                "title": {
                    "type": "string"
                },
                "tmdb_rating": {
                    "type": "number"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.GenreResponse": {
            "type": "object",
            "properties": {
                "name_en": {
                    "type": "string"
                },
                "name_ru": {
                    "type": "string"
                },
                "slug": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.MyFilmCounts": {
            "type": "object",
            "properties": {
                "rated": {
                    "type": "integer"
                },
                "watched": {
                    "type": "integer"
                },
                "watching": {
                    "type": "integer"
                },
                "watchlist": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.MyFilmItem": {
            "type": "object",
            "properties": {
                "added_at": {
                    "type": "string"
                },
                "description": {
                    "type": "string"
                },
                "genres": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "local_rating": {
                    "type": "number"
                },
                "local_votes": {
                    "type": "integer"
                },
                "poster_url": {
                    "type": "string"
                },
                "rate": {
                    "type": "integer"
                },
                "runtime": {
                    "type": "integer"
                },
                "series_rating": {
                    "type": "number"
                },
                "title": {
                    "type": "string"
                },
                "tmdb_rating": {
                    "type": "number"
                },
                "watched_at": {
                    "type": "string"
                },
                "watchlisted": {
                    "type": "boolean"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.MyFilmsPage": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.MyFilmItem"
                    }
                },
                "next_cursor": {
                    "type": "string"
                },
                "total": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.PersonCard": {
            "type": "object",
            "properties": {
                "film_count": {
                    "type": "integer"
                },
                "films": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmographyItem"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "name": {
                    "type": "string"
                },
                "original_name": {
                    "type": "string"
                },
                "photo_url": {
                    "type": "string"
                },
                "roles": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                }
            }
        },
        "lifelog-api_internal_service.PersonSearchItem": {
            "type": "object",
            "properties": {
                "film_count": {
                    "type": "integer"
                },
                "id": {
                    "type": "integer"
                },
                "known_for": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "name": {
                    "type": "string"
                },
                "original_name": {
                    "type": "string"
                },
                "photo_url": {
                    "type": "string"
                },
                "roles": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                }
            }
        },
        "lifelog-api_internal_service.PlacePage": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.PlaceResponse"
                    }
                },
                "next_cursor": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.PlaceResponse": {
            "type": "object",
            "properties": {
                "address": {
                    "type": "string"
                },
                "category": {
                    "type": "string"
                },
                "city_id": {
                    "type": "integer"
                },
                "city_name": {
                    "type": "string"
                },
                "country_code": {
                    "type": "string"
                },
                "country_name": {
                    "type": "string"
                },
                "id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "lat": {
                    "type": "number"
                },
                "lon": {
                    "type": "number"
                },
                "moderation_status": {
                    "$ref": "#/definitions/lifelog-api_internal_model.ModerationStatus"
                },
                "name": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.RecommendedFilms": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmSearchItem"
                    }
                }
            }
        },
        "lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_FilmCatalogItem": {
            "type": "object",
            "properties": {
                "facets": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.Facet"
                    }
                },
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmCatalogItem"
                    }
                },
                "next_cursor": {
                    "type": "string"
                },
                "total": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_FilmSearchItem": {
            "type": "object",
            "properties": {
                "facets": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.Facet"
                    }
                },
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmSearchItem"
                    }
                },
                "next_cursor": {
                    "type": "string"
                },
                "total": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.SearchPage-lifelog-api_internal_service_PersonSearchItem": {
            "type": "object",
            "properties": {
                "facets": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.Facet"
                    }
                },
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.PersonSearchItem"
                    }
                },
                "next_cursor": {
                    "type": "string"
                },
                "total": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.SharedListCard": {
            "type": "object",
            "properties": {
                "created_at": {
                    "type": "string"
                },
                "draws": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.SharedListDraw"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "is_owner": {
                    "type": "boolean"
                },
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.SharedListItem"
                    }
                },
                "items_count": {
                    "type": "integer"
                },
                "last_draw": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SharedListDraw"
                },
                "members": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.UserRef"
                    }
                },
                "members_count": {
                    "type": "integer"
                },
                "owner": {
                    "$ref": "#/definitions/lifelog-api_internal_service.UserRef"
                },
                "title": {
                    "type": "string"
                },
                "updated_at": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.SharedListDraw": {
            "type": "object",
            "properties": {
                "count": {
                    "type": "integer"
                },
                "created_at": {
                    "type": "string"
                },
                "drawn_by": {
                    "$ref": "#/definitions/lifelog-api_internal_service.UserRef"
                },
                "films": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmSearchItem"
                    }
                },
                "id": {
                    "type": "integer"
                },
                "mode": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.SharedListDrawsPage": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.SharedListDraw"
                    }
                }
            }
        },
        "lifelog-api_internal_service.SharedListItem": {
            "type": "object",
            "properties": {
                "added_at": {
                    "type": "string"
                },
                "added_by": {
                    "$ref": "#/definitions/lifelog-api_internal_service.UserRef"
                },
                "film": {
                    "$ref": "#/definitions/lifelog-api_internal_service.FilmSearchItem"
                }
            }
        },
        "lifelog-api_internal_service.SharedListSummary": {
            "type": "object",
            "properties": {
                "created_at": {
                    "type": "string"
                },
                "id": {
                    "type": "integer"
                },
                "is_owner": {
                    "type": "boolean"
                },
                "items_count": {
                    "type": "integer"
                },
                "last_draw": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SharedListDraw"
                },
                "members_count": {
                    "type": "integer"
                },
                "owner": {
                    "$ref": "#/definitions/lifelog-api_internal_service.UserRef"
                },
                "title": {
                    "type": "string"
                },
                "updated_at": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.SharedListsPage": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.SharedListSummary"
                    }
                }
            }
        },
        "lifelog-api_internal_service.SimilarFilms": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_service.FilmSearchItem"
                    }
                },
                "source": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_service.StartSyncRequest": {
            "type": "object",
            "required": [
                "year_from",
                "year_to"
            ],
            "properties": {
                "year_from": {
                    "type": "integer"
                },
                "year_to": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.SyncState": {
            "type": "string",
            "enum": [
                "idle",
                "running",
                "succeeded",
                "failed"
            ],
            "x-enum-varnames": [
                "SyncStateIdle",
                "SyncStateRunning",
                "SyncStateSucceeded",
                "SyncStateFailed"
            ]
        },
        "lifelog-api_internal_service.SyncStats": {
            "type": "object",
            "properties": {
                "failed": {
                    "type": "integer"
                },
                "fetched": {
                    "type": "integer"
                },
                "requested": {
                    "type": "integer"
                },
                "saved": {
                    "type": "integer"
                },
                "skipped": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.SyncStatusResponse": {
            "type": "object",
            "properties": {
                "error": {
                    "type": "string"
                },
                "finished_at": {
                    "type": "string"
                },
                "index_stale": {
                    "description": "IndexStale — импорт записал новые карточки, а Elasticsearch о них не\nзнает: ручка индекс не трогает, переиндексацию делает cmd/indexer.",
                    "type": "boolean"
                },
                "run_id": {
                    "type": "string"
                },
                "started_at": {
                    "type": "string"
                },
                "state": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SyncState"
                },
                "stats": {
                    "$ref": "#/definitions/lifelog-api_internal_service.SyncStats"
                },
                "year_from": {
                    "type": "integer"
                },
                "year_to": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_service.UpdatePlaceRequest": {
            "type": "object",
            "required": [
                "address",
                "category",
                "name"
            ],
            "properties": {
                "address": {
                    "type": "string",
                    "maxLength": 128,
                    "example": "Ленина"
                },
                "category": {
                    "type": "string",
                    "maxLength": 32,
                    "example": "Кафе"
                },
                "city_id": {
                    "type": "integer"
                },
                "country_code": {
                    "description": "CountryCode и CityID берутся из справочника географии, а не вводятся\nстрокой: иначе «Таиланд», «Тайланд» и «Thailand» станут тремя странами\nи фильтр по стране начнёт врать.",
                    "type": "string",
                    "example": "TH"
                },
                "kind": {
                    "description": "Kind — тип места из фиксированного перечня: food, culture, park, stay, other.\nНеобязателен: без него место получает other.",
                    "type": "string",
                    "example": "food"
                },
                "lat": {
                    "type": "number"
                },
                "lon": {
                    "type": "number"
                },
                "name": {
                    "type": "string",
                    "maxLength": 128,
                    "example": "Alex"
                }
            }
        },
        "lifelog-api_internal_service.UserRef": {
            "type": "object",
            "properties": {
                "auth_user_id": {
                    "type": "string",
                    "format": "uuid"
                },
                "name": {
                    "type": "string"
                }
            }
        },
        "lifelog-api_internal_tmdbadd.AddResponse": {
            "type": "object",
            "properties": {
                "created": {
                    "type": "boolean"
                },
                "id": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_tmdbadd.SearchItem": {
            "type": "object",
            "properties": {
                "catalog_id": {
                    "type": "integer"
                },
                "kind": {
                    "type": "string"
                },
                "original_title": {
                    "type": "string"
                },
                "overview": {
                    "type": "string"
                },
                "poster_url": {
                    "type": "string"
                },
                "title": {
                    "type": "string"
                },
                "tmdb_id": {
                    "type": "integer"
                },
                "vote_average": {
                    "type": "number"
                },
                "vote_count": {
                    "type": "integer"
                },
                "year": {
                    "type": "integer"
                }
            }
        },
        "lifelog-api_internal_tmdbadd.SearchResponse": {
            "type": "object",
            "properties": {
                "items": {
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/lifelog-api_internal_tmdbadd.SearchItem"
                    }
                },
                "page": {
                    "type": "integer"
                },
                "total_pages": {
                    "type": "integer"
                }
            }
        }
    },
    "securityDefinitions": {
        "BearerAuth": {
            "description": "Введите токен в формате: Bearer {token}",
            "type": "apiKey",
            "name": "Authorization",
            "in": "header"
        }
    }
}`

var SwaggerInfo = &swag.Spec{
	Version:          "1.0",
	Host:             "",
	BasePath:         "/api/v1",
	Schemes:          []string{},
	Title:            "LifeLog API",
	Description:      "Личный справочник мест",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
	LeftDelim:        "{{",
	RightDelim:       "}}",
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
