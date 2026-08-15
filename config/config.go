package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

var DefaultFiles = []string{".env.local", ".env"}

func Load[T any](files ...string) (T, error) {
	var cfg T

	if err := LoadInto(&cfg, files...); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func MustLoad[T any](files ...string) T {
	cfg, err := Load[T](files...)
	if err != nil {
		panic(err)
	}
	return cfg
}

func LoadInto(dst any, files ...string) error {
	if err := LoadFiles(files...); err != nil {
		return err
	}

	if err := env.Parse(dst); err != nil {
		return fmt.Errorf("config: parse environment: %w", err)
	}

	return nil
}

func LoadFiles(files ...string) error {
	if len(files) == 0 {
		files = DefaultFiles
	}

	for _, file := range files {
		if _, err := os.Stat(file); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("config: stat %s: %w", file, err)
		}

		if err := godotenv.Load(file); err != nil {
			return fmt.Errorf("config: load %s: %w", file, err)
		}
	}

	return nil
}
