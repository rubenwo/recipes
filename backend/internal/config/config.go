package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Ollama   OllamaConfig   `yaml:"ollama"`
	Search   SearchConfig   `yaml:"search"`
	Edamam   EdamamConfig   `yaml:"edamam"`
}

type ServerConfig struct {
	Port       int    `yaml:"port"`
	CORSOrigin string `yaml:"cors_origin"`
	ImagesDir  string `yaml:"images_dir"`
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
	SSLMode  string `yaml:"sslmode"`
}

type OllamaConfig struct {
	Host              string        `yaml:"host"`
	Model             string        `yaml:"model"`
	GenerationTimeout time.Duration `yaml:"generation_timeout"`
	MaxToolIterations int           `yaml:"max_tool_iterations"`
}

type SearchConfig struct {
	Timeout  time.Duration `yaml:"timeout"`
	CacheTTL time.Duration `yaml:"cache_ttl"`
}

type EdamamConfig struct {
	AppID  string `yaml:"app_id"`
	AppKey string `yaml:"app_key"`
}

func (e EdamamConfig) Enabled() bool {
	return e.AppID != "" && e.AppKey != ""
}

func (d DatabaseConfig) ConnString() string {
	return "postgres://" + d.User + ":" + d.Password + "@" + d.Host + ":" +
		itoa(d.Port) + "/" + d.Name + "?sslmode=" + d.SSLMode
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:       8080,
			CORSOrigin: "http://localhost:5173",
			ImagesDir:  "./images",
		},
		Database: DatabaseConfig{
			Host:    "localhost",
			Port:    5432,
			User:    "postgres",
			Name:    "recipes",
			SSLMode: "disable",
		},
		Ollama: OllamaConfig{
			Host:              "http://localhost:11434",
			Model:             "qwen2.5:7b",
			GenerationTimeout: 60 * time.Second,
			MaxToolIterations: 5,
		},
		Search: SearchConfig{
			Timeout:  10 * time.Second,
			CacheTTL: 5 * time.Minute,
		},
	}

	if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		cfg.Ollama.Model = model
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if model := os.Getenv("OLLAMA_MODEL"); model != "" {
		cfg.Ollama.Model = model
	}

	return cfg, nil
}
