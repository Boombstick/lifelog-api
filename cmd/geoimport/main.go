package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lifelog-api/internal/config"
	"lifelog-api/internal/geo"
	"lifelog-api/internal/logging"
	"lifelog-api/internal/storage/postgres"
)

func run() error {
	var (
		dir     = flag.String("dir", "", "куда складывать скачанные дампы (по умолчанию временная папка)")
		skipRu  = flag.Bool("skip-russian", false, "не качать файл переводов на 200 МБ, оставить английские названия")
		minPop  = flag.Int("min-population", 0, "не загружать города меньше указанного населения")
		verbose = flag.Bool("verbose", false, "подробный лог")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := logging.SetupLevel("geoimport", level)

	configuration, err := config.New()
	if err != nil {
		return err
	}

	database, err := postgres.NewPostgres(postgres.ConfigFrom(configuration, configuration.PostgresDatabase, logger))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	started := time.Now()
	loader := geo.NewLoader(*dir, logger)
	repo := postgres.NewGeoRepository(database)

	countries, countryGeonameIDs, err := loader.Countries(ctx)
	if err != nil {
		return err
	}
	logger.Info("страны разобраны", "count", len(countries))

	cities, err := loader.Cities(ctx)
	if err != nil {
		return err
	}
	if *minPop > 0 {
		filtered := cities[:0]
		for _, city := range cities {
			if city.Population >= *minPop {
				filtered = append(filtered, city)
			}
		}
		cities = filtered
	}
	logger.Info("города разобраны", "count", len(cities))

	if !*skipRu {
		wanted := make(map[uint]struct{}, len(cities)+len(countryGeonameIDs))
		for _, city := range cities {
			wanted[city.ID] = struct{}{}
		}
		for id := range countryGeonameIDs {
			wanted[id] = struct{}{}
		}

		names, err := loader.RussianNames(ctx, wanted)
		if err != nil {
			return err
		}
		logger.Info("русские названия найдены", "count", len(names))

		for i := range cities {
			if name, ok := names[cities[i].ID]; ok {
				cities[i].NameRu = name
			}
		}
		byCode := make(map[string]string, len(countryGeonameIDs))
		for geonameID, code := range countryGeonameIDs {
			if name, ok := names[geonameID]; ok {
				byCode[code] = name
			}
		}
		for i := range countries {
			if name, ok := byCode[countries[i].Code]; ok {
				countries[i].NameRu = name
			}
		}
	}

	if err := repo.SaveCountries(ctx, countries); err != nil {
		return err
	}
	if err := repo.SaveCities(ctx, cities); err != nil {
		return err
	}

	logger.Info("справочник загружен",
		"страны", len(countries),
		"города", len(cities),
		"elapsed", time.Since(started).Round(time.Second))
	return nil
}

func main() {
	if err := run(); err != nil {
		slog.Error("geoimport", "error", err)
		os.Exit(1)
	}
}
