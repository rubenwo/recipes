package models

import "time"

type OllamaProvider struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Model     string    `json:"model"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type AppSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
