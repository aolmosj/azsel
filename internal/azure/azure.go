package azure

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type AccountInfo struct {
	TenantID string `json:"tenantId"`
	Name     string `json:"name"`
	User     struct {
		Name string `json:"name"`
	} `json:"user"`
}

func Login(tenantID, configDir, extensionsDir string, useDeviceCode bool) error {
	args := []string{"login", "--tenant", tenantID}
	if useDeviceCode {
		args = append(args, "--use-device-code")
	}
	cmd := exec.Command("az", args...)
	cmd.Env = append(os.Environ(), "AZURE_CONFIG_DIR="+configDir, "AZURE_EXTENSION_DIR="+extensionsDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func AccountShow(configDir, extensionsDir string) (*AccountInfo, error) {
	cmd := exec.Command("az", "account", "show", "--output", "json")
	cmd.Env = append(os.Environ(), "AZURE_CONFIG_DIR="+configDir, "AZURE_EXTENSION_DIR="+extensionsDir)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("az account show: %w", err)
	}
	var info AccountInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parsing account info: %w", err)
	}
	return &info, nil
}
