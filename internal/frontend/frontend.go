// Package frontend launches the Electrobun curate UI and collects the user's
// deletion choices. IPC is done via two temp JSON files whose paths are passed
// to the Bun process via environment variables:
//   - PM_INPUT  (Go → Bun): groups + photo metadata
//   - PM_OUTPUT (Bun → Go): list of paths chosen for deletion
package frontend

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jolo/photo-manager/internal/similarity"
	"github.com/jolo/photo-manager/internal/storage"
)

// curateInput is the JSON payload written for the Electrobun process.
type curateInput struct {
	Groups  []similarity.Group            `json:"groups"`
	Photos  map[string]*storage.PhotoMeta `json:"photos"`
	LibRoot string                        `json:"lib_root"`
}

// curateOutput is the JSON payload read back from the Electrobun process.
type curateOutput struct {
	ToDelete []string `json:"to_delete"`
}

// Run opens the Electrobun curate window and returns the paths the user
// chose to delete. It blocks until the window is closed.
func Run(groups []similarity.Group, metas map[string]*storage.PhotoMeta, libRoot string) ([]string, error) {
	frontendDir, err := resolveFrontendDir()
	if err != nil {
		return nil, err
	}

	// Write input temp file.
	inputFile, err := os.CreateTemp("", "photo-manager-input-*.json")
	if err != nil {
		return nil, fmt.Errorf("creating input temp file: %w", err)
	}
	inputPath := inputFile.Name()
	defer os.Remove(inputPath)

	if err := json.NewEncoder(inputFile).Encode(curateInput{
		Groups:  groups,
		Photos:  metas,
		LibRoot: libRoot,
	}); err != nil {
		inputFile.Close()
		return nil, fmt.Errorf("writing input JSON: %w", err)
	}
	inputFile.Close()

	// Reserve output temp file path (Bun will write it).
	outputFile, err := os.CreateTemp("", "photo-manager-output-*.json")
	if err != nil {
		return nil, fmt.Errorf("creating output temp file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	// Spawn `bun run curate` from the frontend directory.
	// IPC paths are passed as env vars because electrobun dev doesn't forward argv.
	cmd := exec.Command("bun", "run", "curate")
	cmd.Dir = frontendDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"PM_INPUT="+inputPath,
		"PM_OUTPUT="+outputPath,
	)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("electrobun process: %w", err)
	}

	// Read result.
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("reading output JSON: %w", err)
	}
	if len(data) == 0 {
		return nil, nil // user closed without confirming
	}

	var out curateOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing output JSON: %w", err)
	}
	return out.ToDelete, nil
}

// resolveFrontendDir finds the frontend/ directory via:
//  1. PHOTO_MANAGER_FRONTEND env var (must point to the frontend/ dir)
//  2. {executable dir}/frontend/
//  3. ./frontend/ (development — go run from repo root)
func resolveFrontendDir() (string, error) {
	const rel = "frontend"

	if v := os.Getenv("PHOTO_MANAGER_FRONTEND"); v != "" {
		return v, nil
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	if _, err := os.Stat(rel); err == nil {
		return rel, nil
	}

	return "", fmt.Errorf(
		"cannot find frontend directory; set PHOTO_MANAGER_FRONTEND=<path/to/frontend/>",
	)
}
