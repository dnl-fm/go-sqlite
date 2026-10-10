package consumers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GoLatestResolver asks the installed Go toolchain for the latest version.
type GoLatestResolver struct{}

// Latest returns the version reported by go list for go-sqlite@latest.
func (GoLatestResolver) Latest(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, "go", "list", "-m", "-json", ModulePath+"@latest")
	command.Env = replaceEnvironmentValue(os.Environ(), "GOWORK", "off")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			details := strings.TrimSpace(string(exitErr.Stderr))
			if details != "" {
				return "", fmt.Errorf("go list: %s", details)
			}
		}
		return "", fmt.Errorf("go list: %w", err)
	}

	var module struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(output, &module); err != nil {
		return "", fmt.Errorf("decode go list response: %w", err)
	}
	if module.Version == "" {
		return "", fmt.Errorf("go list returned no version for %s@latest", ModulePath)
	}
	return module.Version, nil
}

func replaceEnvironmentValue(environment []string, key, value string) []string {
	prefix := key + "="
	replaced := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			replaced = append(replaced, entry)
		}
	}
	return append(replaced, prefix+value)
}
