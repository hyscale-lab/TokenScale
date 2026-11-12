package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/otiai10/copy"
)

type TmpDirManager struct {
	tmpDirPath string
}

// NewTmpDirManager creates a new temporary directory under /tmp/tmp_config_<uuid>
// and copies all contents from the source directory into it.
func NewTmpDirManager(srcDirPath string) (*TmpDirManager, error) {
	// check if srcDirPath exists
	if info, err := os.Stat(srcDirPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source directory %q does not exist", srcDirPath)
		}
		return nil, fmt.Errorf("cannot stat %q: %w", srcDirPath, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("source path %q is not a directory", srcDirPath)
	}

	id := uuid.New().String()
	tmpDirPath := path.Join("/tmp", fmt.Sprintf("tmp_config_%s", id))

	// Create temporary directory
	err := os.MkdirAll(tmpDirPath, 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// Copy all files and directories from srcDirPath to tmpDirPath
	err = copy.Copy(srcDirPath, tmpDirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy files: %w", err)
	}

	return &TmpDirManager{tmpDirPath: tmpDirPath}, nil
}

// TmpDirPath returns the path to the temporary directory
func (t *TmpDirManager) TmpDirPath() string {
	return t.tmpDirPath
}

// DeleteTmpDir deletes the temporary directory and its contents
func (t *TmpDirManager) DeleteTmpDir() error {
	return os.RemoveAll(t.tmpDirPath)
}

// UpdateParams placeholder method to be implemented later
func (t *TmpDirManager) UpdateParams(updateParamsMap map[string]interface{}) error {
	// TODO: implement logic to update parameters in config files
	for key, newValue := range updateParamsMap {
		parts := strings.Split(key, "::")
		if len(parts) != 2 {
			return fmt.Errorf("invalid parameter key format: %s", key)
		}

		relPath := parts[0]
		fieldName := parts[1]

		absPath := path.Join(t.tmpDirPath, relPath)

		// Read the JSON file
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", absPath, err)
		}

		// Unmarshal to map[string]interface{}
		var jsonObj map[string]interface{}
		if err := json.Unmarshal(content, &jsonObj); err != nil {
			return fmt.Errorf("failed to unmarshal JSON from %s: %w", absPath, err)
		}

		// Check if the field exists
		oldValue, exists := jsonObj[fieldName]
		if !exists {
			return fmt.Errorf("field %s not found in %s", fieldName, absPath)
		}

		// Type check
		if reflect.TypeOf(oldValue) != reflect.TypeOf(newValue) {
			return fmt.Errorf("type mismatch for field %s in %s: expected %T, got %T", fieldName, absPath, oldValue, newValue)
		}

		// Update the field
		jsonObj[fieldName] = newValue

		// Marshal back to JSON
		updatedJSON, err := json.MarshalIndent(jsonObj, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal updated JSON for %s: %w", absPath, err)
		}

		// Write back to file
		if err := os.WriteFile(absPath, updatedJSON, 0644); err != nil {
			return fmt.Errorf("failed to write updated JSON to %s: %w", absPath, err)
		}
	}

	return nil
}