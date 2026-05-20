package mapdata

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mascotl/livemap/internal/mapbundle"
	_ "modernc.org/sqlite"
)

type MapInfo struct {
	Path       string
	Game       string
	MapVersion string
	Categories []Category
}

type Category struct {
	Name  string
	Image []byte
	Thumb []byte
}

type PinProjectInfo struct {
	Path       string
	Name       string
	Game       string
	MapVersion string
	Points     []PinPoint
}

type PinPoint struct {
	Category string
	X        int
	Y        int
	Desc     string
}

func LoadMap(path string) (MapInfo, error) {
	if !strings.EqualFold(filepath.Ext(path), ".map") {
		return MapInfo{}, fmt.Errorf("地图文件必须是 .map: %s", path)
	}
	bundle, err := mapbundle.Open(path)
	if err != nil {
		return MapInfo{}, err
	}
	dbReader, err := bundle.SQLite()
	if err != nil {
		return MapInfo{}, err
	}
	defer dbReader.Close()

	db, cleanup, err := openSQLiteReader(dbReader)
	if err != nil {
		return MapInfo{}, err
	}
	defer cleanup()
	defer db.Close()

	info := MapInfo{Path: path}
	meta, err := readMetadata(db)
	if err != nil {
		return MapInfo{}, err
	}
	info.Game = meta["game"]
	info.MapVersion = meta["map_version"]
	if info.Game == "" {
		info.Game = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	rows, err := db.Query(`SELECT point_name, img, thumb FROM points ORDER BY id`)
	if err != nil {
		return MapInfo{}, fmt.Errorf("读取地图分类失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Name, &c.Image, &c.Thumb); err != nil {
			return MapInfo{}, err
		}
		info.Categories = append(info.Categories, c)
	}
	return info, rows.Err()
}

func LoadPinProject(path string) (PinProjectInfo, error) {
	if !strings.EqualFold(filepath.Ext(path), ".gmp") {
		return PinProjectInfo{}, fmt.Errorf("标点文件必须是 .gmp: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return PinProjectInfo{}, err
	}
	defer file.Close()
	header := make([]byte, 10)
	if _, err := io.ReadFull(file, header); err != nil {
		return PinProjectInfo{}, err
	}
	if string(header[:8]) != "mappoint" {
		return PinProjectInfo{}, fmt.Errorf("GMP 魔数不匹配")
	}

	db, cleanup, err := openSQLiteReader(file)
	if err != nil {
		return PinProjectInfo{}, err
	}
	defer cleanup()
	defer db.Close()
	if err := validateGMP(db); err != nil {
		return PinProjectInfo{}, err
	}

	info := PinProjectInfo{
		Path: path,
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}
	meta, err := readMetadata(db)
	if err != nil {
		return PinProjectInfo{}, err
	}
	info.Game = meta["game"]
	info.MapVersion = meta["map_version"]

	rows, err := db.Query(`SELECT COALESCE(cat_name, ''), x, y, COALESCE(desc, '') FROM points ORDER BY id`)
	if err != nil {
		return PinProjectInfo{}, fmt.Errorf("读取标点失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p PinPoint
		if err := rows.Scan(&p.Category, &p.X, &p.Y, &p.Desc); err != nil {
			return PinProjectInfo{}, err
		}
		info.Points = append(info.Points, p)
	}
	return info, rows.Err()
}

func DecodeImage(data []byte) (*image.RGBA, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return rgba, nil
}

func openSQLiteReader(r io.Reader) (*sql.DB, func(), error) {
	tmp, err := os.CreateTemp("", "livemap-*.sqlite")
	if err != nil {
		return nil, nil, err
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return nil, nil, err
	}
	cleanup := func() { _ = os.Remove(path) }
	data, err := io.ReadAll(r)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		cleanup()
		return nil, nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return db, cleanup, nil
}

func readMetadata(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM metadata`)
	if err != nil {
		return nil, fmt.Errorf("读取 metadata 失败: %w", err)
	}
	defer rows.Close()
	meta := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		meta[k] = v
	}
	return meta, rows.Err()
}

func validateGMP(db *sql.DB) error {
	required := map[string]bool{
		"metadata":         false,
		"point_categories": false,
		"points":           false,
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name == "sqlite_sequence" {
			continue
		}
		if _, ok := required[name]; !ok {
			return fmt.Errorf("GMP 包含不支持的业务表: %s", name)
		}
		required[name] = true
	}
	for name, ok := range required {
		if !ok {
			return fmt.Errorf("GMP 缺少业务表: %s", name)
		}
	}
	return nil
}
