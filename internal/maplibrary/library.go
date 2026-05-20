package maplibrary

import (
	"os"

	"github.com/mascotl/livemap/internal/appconfig"
	"github.com/mascotl/livemap/internal/mapdata"
	"github.com/mascotl/livemap/internal/mapoverlay"
)

func Clean(cfg *appconfig.Config) {
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
	applySelectedMap(cfg)
}

func AddMap(cfg *appconfig.Config, path string) error {
	info, err := mapdata.LoadMap(path)
	if err != nil {
		return err
	}
	found := false
	for i := range cfg.Resources.Maps {
		if cfg.Resources.Maps[i].Path == path {
			cfg.Resources.Maps[i].Game = info.Game
			cfg.Resources.Maps[i].MapVersion = info.MapVersion
			cfg.Resources.Maps[i].Selected = true
			found = true
		} else {
			cfg.Resources.Maps[i].Selected = false
		}
	}
	if !found {
		cfg.Resources.Maps = append(cfg.Resources.Maps, appconfig.MapResource{
			Path:       path,
			Game:       info.Game,
			MapVersion: info.MapVersion,
			Selected:   true,
		})
	}
	applySelectedMap(cfg)
	return nil
}

func AddPin(cfg *appconfig.Config, path string) error {
	info, err := mapdata.LoadPinProject(path)
	if err != nil {
		return err
	}
	found := false
	for i := range cfg.Resources.Pins {
		if cfg.Resources.Pins[i].Path != path {
			continue
		}
		cfg.Resources.Pins[i].Name = info.Name
		cfg.Resources.Pins[i].Game = info.Game
		cfg.Resources.Pins[i].MapVersion = info.MapVersion
		found = true
	}
	if !found {
		cfg.Resources.Pins = append(cfg.Resources.Pins, appconfig.PinResource{
			Path:       path,
			Name:       info.Name,
			Game:       info.Game,
			MapVersion: info.MapVersion,
			Enabled:    false,
		})
	}
	return nil
}

func SelectMap(cfg *appconfig.Config, path string) {
	for i := range cfg.Resources.Maps {
		cfg.Resources.Maps[i].Selected = cfg.Resources.Maps[i].Path == path
	}
	applySelectedMap(cfg)
}

func TogglePin(cfg *appconfig.Config, path string) {
	game, version := selectedMapIdentity(cfg)
	for i := range cfg.Resources.Pins {
		if cfg.Resources.Pins[i].Path != path {
			continue
		}
		if cfg.Resources.Pins[i].Game == game && cfg.Resources.Pins[i].MapVersion == version {
			cfg.Resources.Pins[i].Enabled = !cfg.Resources.Pins[i].Enabled
		}
	}
}

func RuntimeState(cfg appconfig.Config) mapoverlay.ResourceState {
	Clean(&cfg)
	state := mapoverlay.ResourceState{}
	game, version := selectedMapIdentity(&cfg)
	for _, item := range cfg.Resources.Maps {
		state.Maps = append(state.Maps, mapoverlay.MapChoice(item))
	}
	for _, item := range cfg.Resources.Pins {
		if item.Game != game || item.MapVersion != version {
			continue
		}
		state.Pins = append(state.Pins, mapoverlay.PinChoice(item))
	}
	if game == "" {
		return state
	}
	for _, item := range cfg.Resources.Maps {
		if !item.Selected {
			continue
		}
		info, err := mapdata.LoadMap(item.Path)
		if err != nil {
			return state
		}
		for _, c := range info.Categories {
			state.Categories = append(state.Categories, mapoverlay.PinCategory{
				Name:      c.Name,
				Image:     c.Image,
				Thumb:     c.Thumb,
				HasVisual: len(c.Thumb) > 0 || len(c.Image) > 0,
			})
		}
		break
	}
	for _, item := range cfg.Resources.Pins {
		if !item.Enabled || item.Game != game || item.MapVersion != version {
			continue
		}
		info, err := mapdata.LoadPinProject(item.Path)
		if err != nil {
			continue
		}
		for _, p := range info.Points {
			state.Markers = append(state.Markers, mapoverlay.PinMarker(p))
		}
	}
	return state
}

func applySelectedMap(cfg *appconfig.Config) {
	cfg.MapMatching.WorldMapPath = ""
	selected := false
	for i := range cfg.Resources.Maps {
		if cfg.Resources.Maps[i].Selected && !selected {
			cfg.MapMatching.WorldMapPath = cfg.Resources.Maps[i].Path
			selected = true
			continue
		}
		if cfg.Resources.Maps[i].Selected {
			cfg.Resources.Maps[i].Selected = false
		}
	}
}

func selectedMapIdentity(cfg *appconfig.Config) (string, string) {
	for _, item := range cfg.Resources.Maps {
		if item.Selected {
			return item.Game, item.MapVersion
		}
	}
	return "", ""
}
