package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/maxischmaxi/qsnap/internal/tools"
	"gopkg.in/yaml.v3"
)

type Size struct {
	Name   string `yaml:"name" json:"name"`
	Width  int    `yaml:"width" json:"width"`
	Height int    `yaml:"height" json:"height"`
}

type Sizes struct {
	Strings []string
	Structs []Size
	One     *Size
}

func (s *Sizes) UnmarshalYAML(unmarshal func(any) error) error {
	// 1) Versuche []string
	var asStrings []string
	if err := unmarshal(&asStrings); err == nil {
		s.Strings = asStrings
		return nil
	}

	// 2) Versuche []Size (optional, robuste Erweiterung)
	var asStructs []Size
	if err := unmarshal(&asStructs); err == nil {
		s.Structs = asStructs
		return nil
	}

	// 3) Versuche einzelnes Size-Objekt
	var one Size
	if err := unmarshal(&one); err == nil && (one.Width != 0 || one.Height != 0) {
		s.One = &one
		return nil
	}

	return fmt.Errorf("unsupported format for sizes: expected []string, Size, or []Size")
}

func (s Sizes) AsStrings() []string { return s.Strings }

func (s Sizes) AsSizes() []Size {
	if s.One != nil {
		return []Size{*s.One}
	}
	return s.Structs
}

type DiffPixelColor struct {
	R int `yaml:"r" json:"r"`
	G int `yaml:"g" json:"g"`
	B int `yaml:"b" json:"b"`
}

type OsnapBaseConfig struct {
	BaseURL           string         `yaml:"baseUrl" json:"baseUrl"`
	FullScreen        bool           `yaml:"fullScreen" json:"fullScreen"`
	Threshold         int            `yaml:"threshold" json:"threshold"`
	Retry             int            `yaml:"retry" json:"retry"`
	SnapshotDirectory string         `yaml:"snapshotDirectory" json:"snapshotDirectory"`
	TestPattern       string         `yaml:"testPattern" json:"testPattern"`
	IgnorePatterns    []string       `yaml:"ignorePatterns" json:"ignorePatterns"`
	DefaultSizes      []Size         `yaml:"defaultSizes" json:"defaultSizes"`
	DiffPixelColor    DiffPixelColor `yaml:"diffPixelColor" json:"diffPixelColor"`
}

type Action struct {
	At       *[]string `yaml:"@,omitempty" json:"@,omitempty"`
	Action   string    `yaml:"action" json:"action"` // wait, click
	Timeout  *int      `yaml:"timeout" json:"timeout"`
	Selector *string   `yaml:"selector" json:"selector"`
}

type OsnapConfig struct {
	Name      string    `yaml:"name" json:"name"`
	URL       string    `yaml:"url" json:"url"`
	Sizes     Sizes     `yaml:"sizes" json:"sizes"`
	Actions   []*Action `yaml:"actions" json:"actions"`
	Threshold *int      `yaml:"threshold,omitempty" json:"threshold,omitempty"`
	Retry     int       `yaml:"retry" json:"retry"`
	Width     int
	Height    int
}

func NewOsnapBaseConfig(baseConfigPath string) (*OsnapBaseConfig, error) {
	config := &OsnapBaseConfig{}

	path, err := tools.ExpandPath(baseConfigPath)
	if err != nil {
		return nil, err
	}

	if !tools.FileExists(path) {
		return nil, fmt.Errorf("base config file does not exist: %s", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	if err := dec.Decode(config); err != nil {
		if errors.Is(err, io.EOF) {
			return config, nil
		}
		return nil, err
	}

	if err := tools.EnsureEOF(dec); err != nil {
		return nil, err
	}

	config.SnapshotDirectory, err = tools.ExpandPath(config.SnapshotDirectory)
	if err != nil {
		return nil, err
	}

	if len(config.DefaultSizes) == 0 {
		return nil, fmt.Errorf("at least one default size must be specified")
	}

	for i, s := range config.DefaultSizes {
		if s.Width <= 0 || s.Height <= 0 {
			return nil, fmt.Errorf("invalid default size at index %d: width and height must be positive integers", i)
		}
	}

	if config.Threshold < 0 || config.Threshold > 100 {
		return nil, fmt.Errorf("threshold must be between 0 and 100")
	}

	if config.Retry < 0 {
		return nil, fmt.Errorf("retry must be non-negative")
	}

	if config.TestPattern == "" {
		return nil, fmt.Errorf("testPattern must be specified")
	}

	if config.SnapshotDirectory == "" {
		return nil, fmt.Errorf("snapshotDirectory must be specified")
	}

	if config.DiffPixelColor.R < 0 || config.DiffPixelColor.R > 255 ||
		config.DiffPixelColor.G < 0 || config.DiffPixelColor.G > 255 ||
		config.DiffPixelColor.B < 0 || config.DiffPixelColor.B > 255 {
		return nil, fmt.Errorf("diffPixelColor values must be between 0 and 255")
	}

	return config, nil
}

func (cfg *OsnapBaseConfig) NewOsnapConfig(configPath string) ([]*OsnapConfig, error) {
	var configs []*OsnapConfig

	if !tools.FileExists(configPath) {
		return nil, fmt.Errorf("config file does not exist: %s", configPath)
	}

	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	if err := dec.Decode(&configs); err != nil {
		if errors.Is(err, io.EOF) {
			return configs, nil
		}
		return nil, err
	}

	if err := tools.EnsureEOF(dec); err != nil {
		return nil, err
	}

	var res []*OsnapConfig

	for _, c := range configs {
		if ss := c.Sizes.AsStrings(); len(ss) > 0 {
			for _, s := range ss {
				for _, ds := range cfg.DefaultSizes {
					if s == ds.Name {
						newC := *c
						newC.Width = ds.Width
						newC.Height = ds.Height

						res = append(res, &newC)
						break
					}
				}
			}
		} else if sz := c.Sizes.AsSizes(); len(sz) > 0 {
			for _, s := range sz {
				newC := *c
				newC.Width = s.Width
				newC.Height = s.Height

				res = append(res, &newC)
			}
		} else if c.Sizes.One != nil {
			s := c.Sizes.One
			newC := *c
			newC.Width = s.Width
			newC.Height = s.Height

			res = append(res, &newC)
		} else {
			for _, s := range cfg.DefaultSizes {
				newC := *c
				newC.Width = s.Width
				newC.Height = s.Height

				res = append(res, &newC)
			}
		}
	}

	return res, nil
}

func (cfg *OsnapBaseConfig) FindAndParseConfigs(root string) ([]*OsnapConfig, error) {
	var results []*OsnapConfig
	var aggErr error

	path, err := tools.ExpandPath(root)
	if err != nil {
		return nil, err
	}

	err = filepath.WalkDir(path, func(path string, d os.DirEntry, wErr error) error {
		if wErr != nil {
			aggErr = errors.Join(aggErr, fmt.Errorf("error accessing path %q: %v", path, wErr))
			return nil
		}

		if d.IsDir() {
			name := d.Name()

			if slices.Contains(cfg.IgnorePatterns, name) {
				return filepath.SkipDir
			}

			if strings.HasPrefix(name, ".") && name != "." && name != ".." {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(d.Name(), ".osnap.yaml") {
			return nil
		}

		configs, err := cfg.NewOsnapConfig(path)
		if err != nil {
			aggErr = errors.Join(aggErr, fmt.Errorf("error parsing config %q: %v", path, err))
			return nil
		}

		for i := range configs {
			results = append(results, configs[i])
		}

		return nil
	})

	aggErr = errors.Join(aggErr, err)
	return results, aggErr
}
