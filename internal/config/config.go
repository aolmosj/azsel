package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Tenant struct {
	Name      string `json:"name"`
	TenantID  string `json:"tenantId"`
	ConfigDir string `json:"configDir"`
}

type Config struct {
	Tenants []Tenant `json:"tenants"`
}

func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	dir := filepath.Join(home, ".azsel")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating base directory: %w", err)
	}
	return dir, nil
}

func EnvFile() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ".switch"), nil
}

func WriteEnv(lines string) error {
	path, err := EnvFile()
	if err != nil {
		return err
	}
	if os.Getenv("AZSEL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[azsel-debug-go] writing %s\n", path)
	}
	return os.WriteFile(path, []byte(lines), 0644)
}

func ConfigPath() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "config.json"), nil
}

func ExtensionsDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "extensions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating extensions directory: %w", err)
	}
	return dir, nil
}

func TenantsDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "tenants")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating tenants directory: %w", err)
	}
	return dir, nil
}

func TenantDir(name string) (string, error) {
	tenants, err := TenantsDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(tenants, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating tenant directory: %w", err)
	}
	return dir, nil
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *Config) FindTenant(name string) *Tenant {
	for i := range c.Tenants {
		if strings.EqualFold(c.Tenants[i].Name, name) {
			return &c.Tenants[i]
		}
	}
	return nil
}

func (c *Config) AddTenant(t Tenant) error {
	if c.FindTenant(t.Name) != nil {
		return fmt.Errorf("tenant %q already exists", t.Name)
	}
	c.Tenants = append(c.Tenants, t)
	return Save(c)
}

func (c *Config) RemoveTenant(name string) error {
	for i := range c.Tenants {
		if strings.EqualFold(c.Tenants[i].Name, name) {
			c.Tenants = append(c.Tenants[:i], c.Tenants[i+1:]...)
			return Save(c)
		}
	}
	return fmt.Errorf("tenant %q not found", name)
}
