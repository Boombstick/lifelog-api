package geo

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	countriesURL = "https://download.geonames.org/export/dump/countryInfo.txt"
	citiesURL    = "https://download.geonames.org/export/dump/cities15000.zip"
	altNamesURL  = "https://download.geonames.org/export/dump/alternateNamesV2.zip"
)

type Loader struct {
	client *http.Client
	logger *slog.Logger
	dir    string
}

func NewLoader(dir string, logger *slog.Logger) *Loader {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "lifelog-geonames")
	}
	return &Loader{
		client: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
		logger: logger,
		dir:    dir,
	}
}

func (l *Loader) Countries(ctx context.Context) ([]Country, map[uint]string, error) {
	path, err := l.download(ctx, countriesURL, "countryInfo.txt")
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var (
		countries = make([]Country, 0, 250)

		byGeonameID = make(map[uint]string, 250)
	)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 17 {
			continue
		}

		code := fields[0]
		name := fields[4]
		continent := fields[8]
		geonameID, _ := strconv.ParseUint(fields[16], 10, 64)

		countries = append(countries, Country{
			Code:      code,
			NameEn:    name,
			NameRu:    name,
			Continent: continent,
		})
		if geonameID > 0 {
			byGeonameID[uint(geonameID)] = code
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return countries, byGeonameID, nil
}

func (l *Loader) Cities(ctx context.Context) ([]City, error) {
	path, err := l.download(ctx, citiesURL, "cities15000.zip")
	if err != nil {
		return nil, err
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	cities := make([]City, 0, 30000)
	for _, entry := range reader.File {
		if !strings.HasSuffix(entry.Name, ".txt") {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}

		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			fields := strings.Split(scanner.Text(), "\t")
			if len(fields) < 15 {
				continue
			}
			id, err := strconv.ParseUint(fields[0], 10, 64)
			if err != nil {
				continue
			}
			lat, _ := strconv.ParseFloat(fields[4], 64)
			lon, _ := strconv.ParseFloat(fields[5], 64)
			population, _ := strconv.Atoi(fields[14])

			cities = append(cities, City{
				ID:          uint(id),
				NameEn:      fields[1],
				CountryCode: fields[8],
				Latitude:    lat,
				Longitude:   lon,
				Population:  population,
			})
		}
		rc.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	return cities, nil
}

func (l *Loader) RussianNames(ctx context.Context, wanted map[uint]struct{}) (map[uint]string, error) {
	path, err := l.download(ctx, altNamesURL, "alternateNamesV2.zip")
	if err != nil {
		return nil, err
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	names := make(map[uint]string, len(wanted))

	preferred := make(map[uint]bool, len(wanted))

	for _, entry := range reader.File {
		if !strings.HasSuffix(entry.Name, ".txt") {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, err
		}

		scanner := bufio.NewScanner(rc)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		lines := 0
		for scanner.Scan() {
			lines++
			if lines%2_000_000 == 0 {
				l.logger.Info("разбор альтернативных имён", "строк", lines, "найдено", len(names))
			}
			if err := ctx.Err(); err != nil {
				rc.Close()
				return nil, err
			}

			fields := strings.Split(scanner.Text(), "\t")
			if len(fields) < 5 || fields[2] != "ru" {
				continue
			}
			id, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				continue
			}
			if _, ok := wanted[uint(id)]; !ok {
				continue
			}

			isPreferred := len(fields) > 5 && fields[5] == "1"
			if existing, ok := names[uint(id)]; ok && !isPreferred {
				_ = existing
				continue
			}
			if preferred[uint(id)] && !isPreferred {
				continue
			}
			names[uint(id)] = fields[3]
			preferred[uint(id)] = isPreferred
		}
		rc.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	return names, nil
}

func (l *Loader) download(ctx context.Context, url, name string) (string, error) {
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(l.dir, name)

	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		l.logger.Info("файл уже скачан", "file", path, "размер_мб", info.Size()/(1024*1024))
		return path, nil
	}

	l.logger.Info("качаю дамп GeoNames", "url", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LifeLog/1.0 (personal project)")

	resp, err := l.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geonames: %s ответил %s", url, resp.Status)
	}

	tmp := path + ".part"
	file, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}

	return path, nil
}
