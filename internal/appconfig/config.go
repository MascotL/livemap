package appconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultConfigPath = "config/livemap.json"

type Config struct {
	ProcessName   string        `json:"process_name"`
	Backend       string        `json:"backend"`
	FPS           int           `json:"fps"`
	MinimapRegion MinimapRegion `json:"minimap_region"`
	MapMatching   MapMatching   `json:"map_matching"`
	Overlay       Overlay       `json:"overlay"`
	Resources     Resources     `json:"resources"`
}

type MinimapRegion struct {
	X    int `json:"x"`
	Y    int `json:"y"`
	Size int `json:"size"`
}

func (r MinimapRegion) Valid() bool {
	return r.Size > 0
}

type MapMatching struct {
	WorldMapPath       string  `json:"world_map_path"`
	GlobalMinimapScale int     `json:"global_minimap_scale"`
	GlobalMapScale     int     `json:"global_map_scale"`
	GlobalWorkers      int     `json:"global_workers"`
	GlobalTimeoutMS    int     `json:"global_timeout_ms"`
	LocalWorkers       int     `json:"local_workers"`
	LocalROI           int     `json:"local_roi"`
	LocalExpandedROI   int     `json:"local_expanded_roi"`
	MatchThreshold     float64 `json:"match_threshold"`
	GlobalSearchHotkey string  `json:"global_search_hotkey"`
	AutoGlobalSearch   bool    `json:"auto_global_search"`
}

type Resources struct {
	Maps []MapResource `json:"maps"`
	Pins []PinResource `json:"pins"`
}

type MapResource struct {
	Path       string `json:"path"`
	Game       string `json:"game"`
	MapVersion string `json:"map_version"`
	Selected   bool   `json:"selected"`
}

type PinResource struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Game       string `json:"game"`
	MapVersion string `json:"map_version"`
	Enabled    bool   `json:"enabled"`
}

type Overlay struct {
	Zoom    float64 `json:"zoom"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Opacity int     `json:"opacity"`
	Topmost bool    `json:"topmost"`
	ShowLog bool    `json:"show_log"`
}

func Default() Config {
	return Config{
		ProcessName: "WeGame.exe",
		Backend:     "",
		FPS:         30,
		Overlay: Overlay{
			Zoom:    1.5,
			Width:   420,
			Height:  420,
			Opacity: 235,
			Topmost: true,
		},
		MapMatching: MapMatching{
			WorldMapPath:       "",
			GlobalMinimapScale: 4,
			GlobalMapScale:     8,
			GlobalWorkers:      4,
			GlobalTimeoutMS:    5000,
			LocalWorkers:       2,
			LocalROI:           300,
			LocalExpandedROI:   400,
			MatchThreshold:     0.55,
			GlobalSearchHotkey: "Delete",
		},
	}
}

func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if cfg.ProcessName == "" {
		cfg.ProcessName = Default().ProcessName
	}
	if cfg.FPS <= 0 {
		cfg.FPS = Default().FPS
	}
	normalizeMapMatching(&cfg)
	normalizeOverlay(&cfg)
	normalizeResources(&cfg)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultConfigPath
	}
	if cfg.ProcessName == "" {
		cfg.ProcessName = Default().ProcessName
	}
	if cfg.FPS <= 0 {
		cfg.FPS = Default().FPS
	}
	normalizeMapMatching(&cfg)
	normalizeOverlay(&cfg)
	normalizeResources(&cfg)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	return nil
}

func normalizeOverlay(cfg *Config) {
	if cfg.Overlay.Zoom <= 0 {
		cfg.Overlay.Zoom = Default().Overlay.Zoom
	}
	if cfg.Overlay.Zoom < 0.5 {
		cfg.Overlay.Zoom = 0.5
	}
	if cfg.Overlay.Zoom > 4 {
		cfg.Overlay.Zoom = 4
	}
	if cfg.Overlay.Width <= 0 {
		cfg.Overlay.Width = Default().Overlay.Width
	}
	if cfg.Overlay.Height <= 0 {
		cfg.Overlay.Height = Default().Overlay.Height
	}
	if cfg.Overlay.Width < 280 {
		cfg.Overlay.Width = 280
	}
	if cfg.Overlay.Height < 240 {
		cfg.Overlay.Height = 240
	}
	if cfg.Overlay.Opacity <= 0 {
		cfg.Overlay.Opacity = Default().Overlay.Opacity
	}
	if cfg.Overlay.Opacity < 51 {
		cfg.Overlay.Opacity = 51
	}
	if cfg.Overlay.Opacity > 255 {
		cfg.Overlay.Opacity = 255
	}
}

func normalizeMapMatching(cfg *Config) {
	def := Default().MapMatching
	if cfg.MapMatching.GlobalMinimapScale <= 0 {
		cfg.MapMatching.GlobalMinimapScale = def.GlobalMinimapScale
	}
	if cfg.MapMatching.GlobalMapScale <= 0 {
		cfg.MapMatching.GlobalMapScale = def.GlobalMapScale
	}
	if cfg.MapMatching.GlobalWorkers <= 0 {
		cfg.MapMatching.GlobalWorkers = def.GlobalWorkers
	}
	if cfg.MapMatching.GlobalTimeoutMS <= 0 {
		cfg.MapMatching.GlobalTimeoutMS = def.GlobalTimeoutMS
	}
	if cfg.MapMatching.LocalWorkers <= 0 {
		cfg.MapMatching.LocalWorkers = def.LocalWorkers
	}
	if cfg.MapMatching.LocalROI <= 0 {
		cfg.MapMatching.LocalROI = def.LocalROI
	}
	if cfg.MapMatching.LocalExpandedROI <= 0 {
		cfg.MapMatching.LocalExpandedROI = def.LocalExpandedROI
	}
	if cfg.MapMatching.MatchThreshold <= 0 {
		cfg.MapMatching.MatchThreshold = def.MatchThreshold
	}
	if cfg.MapMatching.GlobalSearchHotkey == "" {
		cfg.MapMatching.GlobalSearchHotkey = def.GlobalSearchHotkey
	}
}

func normalizeResources(cfg *Config) {
	maps := cfg.Resources.Maps[:0]
	for _, item := range cfg.Resources.Maps {
		if item.Path == "" {
			continue
		}
		if _, err := os.Stat(item.Path); err == nil {
			maps = append(maps, item)
		}
	}
	cfg.Resources.Maps = maps
	pins := cfg.Resources.Pins[:0]
	for _, item := range cfg.Resources.Pins {
		if item.Path == "" {
			continue
		}
		if _, err := os.Stat(item.Path); err == nil {
			pins = append(pins, item)
		}
	}
	cfg.Resources.Pins = pins
	selected := false
	for i := range cfg.Resources.Maps {
		if cfg.Resources.Maps[i].Path == "" {
			continue
		}
		if cfg.Resources.Maps[i].Selected {
			if selected {
				cfg.Resources.Maps[i].Selected = false
				continue
			}
			selected = true
			cfg.MapMatching.WorldMapPath = cfg.Resources.Maps[i].Path
		}
	}
	if !selected {
		cfg.MapMatching.WorldMapPath = ""
	}
}

func ResolvePath(path string) string {
	if path == "" {
		path = DefaultConfigPath
	}
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
