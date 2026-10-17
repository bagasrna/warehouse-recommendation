package assets

import (
	"bytes"
	"embed"
	"io/fs"

	"example.com/simpul-warehouse-selection/internal/catalog"
	"example.com/simpul-warehouse-selection/internal/domain"
)

//go:embed data/catalog.json data/delivery_history.csv web/*
var files embed.FS

func Catalog() (domain.Catalog, error) {
	data, err := files.ReadFile("data/catalog.json")
	if err != nil {
		return domain.Catalog{}, err
	}
	return catalog.Load(bytes.NewReader(data))
}

func Web() (fs.FS, error)         { return fs.Sub(files, "web") }
func HistoryCSV() ([]byte, error) { return files.ReadFile("data/delivery_history.csv") }
