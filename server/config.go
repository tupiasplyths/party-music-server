package server

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	Music  MusicConfig  `yaml:"music"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type MusicConfig struct {
	OutputDevice string `yaml:"output_device"`
	Volume       int    `yaml:"volume"`
	YtDlpPath    string `yaml:"yt_dlp_path"`
	FfplayPath   string `yaml:"ffplay_path"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Music.YtDlpPath == "" {
		if os.PathSeparator == '\\' {
			cfg.Music.YtDlpPath = "./yt-dlp.exe"
		} else {
			cfg.Music.YtDlpPath = "./yt-dlp"
		}
	}
	if cfg.Music.FfplayPath == "" {
		if os.PathSeparator == '\\' {
			cfg.Music.FfplayPath = "./ffplay.exe"
		} else {
			cfg.Music.FfplayPath = "./ffplay"
		}
	}

	return &cfg, nil
}
