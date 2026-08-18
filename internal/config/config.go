package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvHome overrides where azsel keeps its own state. It exists for the same
// reason azsel exists at all: az honours AZURE_CONFIG_DIR, so azsel honours
// AZSEL_HOME. Tests rely on it to stay away from the real ~/.azsel.
const EnvHome = "AZSEL_HOME"

type Tenant struct {
	Name      string `json:"name"`
	TenantID  string `json:"tenantId"`
	ConfigDir string `json:"configDir"`
}

type Config struct {
	Tenants []Tenant `json:"tenants"`
}

// BaseDir reports where azsel keeps its state. It only computes the path —
// nothing is created. Use EnsureBaseDir when the directory has to exist.
func BaseDir() (string, error) {
	if dir := os.Getenv(EnvHome); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".azsel"), nil
}

// EnsureBaseDir returns the base directory, creating it when missing.
func EnsureBaseDir() (string, error) {
	dir, err := BaseDir()
	if err != nil {
		return "", err
	}
	if _, err := ensureDir(dir); err != nil {
		return "", fmt.Errorf("creating base directory: %w", err)
	}
	return dir, nil
}

// inBase joins name onto the base directory without touching the filesystem.
func inBase(name string) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, name), nil
}

// ensureDir creates path when missing and reports whether this call created
// it. Callers that roll back on failure need that distinction: deleting a
// directory somebody else put there would destroy real credentials.
func ensureDir(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return false, err
	}
	return true, nil
}

func ConfigPath() (string, error)    { return inBase("config.json") }
func EnvFile() (string, error)       { return inBase(".switch") }
func ExtensionsDir() (string, error) { return inBase("extensions") }
func TenantsDir() (string, error)    { return inBase("tenants") }

// TenantDir computes a tenant's config directory without creating it.
func TenantDir(name string) (string, error) {
	tenants, err := TenantsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(tenants, name), nil
}

// EnsureExtensionsDir returns the shared extensions directory, creating it
// when missing. Extensions are deliberately shared across every tenant so
// each one only has to be installed once.
func EnsureExtensionsDir() (string, error) {
	dir, err := ExtensionsDir()
	if err != nil {
		return "", err
	}
	if _, err := ensureDir(dir); err != nil {
		return "", fmt.Errorf("creating extensions directory: %w", err)
	}
	return dir, nil
}

// EnsureTenantDir returns a tenant's config directory, creating it when
// missing. created reports whether this call created it.
func EnsureTenantDir(name string) (dir string, created bool, err error) {
	dir, err = TenantDir(name)
	if err != nil {
		return "", false, err
	}
	created, err = ensureDir(dir)
	if err != nil {
		return "", false, fmt.Errorf("creating tenant directory: %w", err)
	}
	return dir, created, nil
}

func WriteEnv(lines string) error {
	if _, err := EnsureBaseDir(); err != nil {
		return err
	}
	path, err := EnvFile()
	if err != nil {
		return err
	}
	if os.Getenv("AZSEL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[azsel-debug-go] writing %s\n", path)
	}
	return os.WriteFile(path, []byte(lines), 0644)
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
	if _, err := EnsureBaseDir(); err != nil {
		return err
	}
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
