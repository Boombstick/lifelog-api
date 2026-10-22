package search

const filmsMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "analysis": {
      "char_filter": {
        "yo_fold": { "type": "mapping", "mappings": ["ё => е", "Ё => Е"] }
      },
      "filter": {
        "russian_stop": { "type": "stop", "stopwords": "_russian_" },
        "russian_stemmer": { "type": "stemmer", "language": "russian" },
        "english_stop": { "type": "stop", "stopwords": "_english_" },
        "english_stemmer": { "type": "stemmer", "language": "english" },
        "edge_ngram_filter": { "type": "edge_ngram", "min_gram": 2, "max_gram": 20 }
      },
      "analyzer": {
        "ru_text": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase", "russian_stop", "russian_stemmer"]
        },
        "en_text": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase", "english_stop", "english_stemmer"]
        },
        "prefix_index": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase", "edge_ngram_filter"]
        },
        "prefix_search": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase"]
        },
        "name_text": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase"]
        }
      },
      "normalizer": {
        "sort_normalizer": { "type": "custom", "char_filter": ["yo_fold"], "filter": ["lowercase"] }
      }
    }
  },
  "mappings": {
    "properties": {
      "id":          { "type": "long" },
      "tmdb_id":     { "type": "long" },
      "imdb_id":     { "type": "keyword" },
      "kind":        { "type": "keyword" },
      "year":        { "type": "integer" },
      "release_date":{ "type": "date" },
      "popularity":  { "type": "float" },
      "runtime":     { "type": "integer" },
      "tmdb_rating": { "type": "float" },
      "tmdb_votes":  { "type": "integer" },
      "local_rating":{ "type": "float" },
      "local_votes": { "type": "integer" },
      "poster_url":  { "type": "keyword", "index": false },
      "title_ru": {
        "type": "text",
        "analyzer": "ru_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      },
      "title_en": {
        "type": "text",
        "analyzer": "en_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      },
      "title_sort":  { "type": "keyword", "normalizer": "sort_normalizer" },
      "synopsis":    { "type": "text", "analyzer": "ru_text" },
      "genres":      { "type": "keyword" },
      "genre_names": { "type": "text", "analyzer": "ru_text" },
      "genre_titles":{ "type": "keyword" },
      "directors": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      },
      "cast": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      },
      "directors_original": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" }
        }
      },
      "cast_original": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" }
        }
      }
    }
  }
}`

const peopleMapping = `{
  "settings": {
    "number_of_shards": 1,
    "number_of_replicas": 0,
    "analysis": {
      "char_filter": {
        "yo_fold": { "type": "mapping", "mappings": ["ё => е", "Ё => Е"] }
      },
      "filter": {
        "edge_ngram_filter": { "type": "edge_ngram", "min_gram": 2, "max_gram": 20 }
      },
      "analyzer": {
        "prefix_index": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase", "edge_ngram_filter"]
        },
        "prefix_search": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase"]
        },
        "name_text": {
          "tokenizer": "standard",
          "char_filter": ["yo_fold"],
          "filter": ["lowercase"]
        }
      },
      "normalizer": {
        "sort_normalizer": { "type": "custom", "char_filter": ["yo_fold"], "filter": ["lowercase"] }
      }
    }
  },
  "mappings": {
    "properties": {
      "id":         { "type": "long" },
      "tmdb_id":    { "type": "long" },
      "roles":      { "type": "keyword" },
      "film_count": { "type": "integer" },
      "popularity": { "type": "float" },
      "known_for":  { "type": "keyword" },
      "photo_url":  { "type": "keyword", "index": false },
      "indexed_at": { "type": "date" },
      "name": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      },
      "original_name": {
        "type": "text",
        "analyzer": "name_text",
        "fields": {
          "prefix": { "type": "text", "analyzer": "prefix_index", "search_analyzer": "prefix_search" },
          "raw":    { "type": "keyword", "normalizer": "sort_normalizer", "ignore_above": 256 }
        }
      }
    }
  }
}`
