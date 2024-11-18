package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type MetadataJSON struct {
	Version      int       `json:"Version"`
	NextUpdate   time.Time `json:"NextUpdate"`
	UpdatedAt    time.Time `json:"UpdatedAt"`
	DownloadedAt time.Time `json:"DownloadedAt"`
}

func readMetadata(metadataPath string) (*MetadataJSON, error) {
	file, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %v", err)
	}

	var metadata MetadataJSON
	if err := json.Unmarshal(file, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata JSON: %v", err)
	}

	return &metadata, nil
}

// copyDir recursively copies a directory tree
func copyDir(src string, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			defer srcFile.Close()

			dstFile, err := os.Create(dstPath)
			if err != nil {
				return err
			}
			defer dstFile.Close()

			if _, err := io.Copy(dstFile, srcFile); err != nil {
				return err
			}
		}
	}

	return nil
}

func backupTrivyDB(cacheDir string) error {
	backupDir := "/tmp/trivy_save"

	os.RemoveAll(backupDir)

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	if err := copyDir(cacheDir, backupDir); err != nil {
		return fmt.Errorf("failed to copy directory: %v", err)
	}

	fmt.Printf("Successfully backed up %s to %s\n", cacheDir, backupDir)
	return nil
}

func updateMetadataNextUpdate(metadataPath string) error {
	metadata, err := readMetadata(metadataPath)
	if err != nil {
		return err
	}

	metadata.NextUpdate = time.Now().Add(4 * time.Hour)

	updatedData, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal updated metadata: %v", err)
	}

	if err := os.WriteFile(metadataPath, updatedData, 0644); err != nil {
		return fmt.Errorf("failed to write updated metadata: %v", err)
	}

	fmt.Printf("Delay NextUpdate timestamp to: %s\n", metadata.NextUpdate.Format(time.RFC3339))
	return nil
}

func restoreTrivyDB(cacheDir string) error {
	backupDir := "/tmp/trivy_save"

	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("failed to remove current directory: %v", err)
	}

	if err := copyDir(backupDir, cacheDir); err != nil {
		return fmt.Errorf("failed to restore from backup: %v", err)
	}

	metadataPath := filepath.Join(cacheDir, "db", "metadata.json")
	if err := updateMetadataNextUpdate(metadataPath); err != nil {
		return fmt.Errorf("failed to update metadata timestamp: %v", err)
	}

	fmt.Printf("Successfully restored %s from %s\n", cacheDir, backupDir)
	return nil
}

func runTrivyUpdateCommand(cacheDir string) error {
	cmd := exec.Command("trivy", "image", "--cache-dir", cacheDir, "--download-db-only")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("trivy command failed: %v\nError output:\n%s", err, stderr.String())
	}
	return nil
}

func main() {
	cacheDir := flag.String("cache-dir", "/tmp/trivy", "Directory to store Trivy cache")
	flag.Parse()

	metadataFile := fmt.Sprintf("%s/db/metadata.json", *cacheDir)
	metadata, err := readMetadata(metadataFile)
	if err != nil {
		fmt.Println("Error reading metadata.json:", err)
		fmt.Println("Executing Trivy DB download...")
		if err := backupTrivyDB(*cacheDir); err != nil {
			fmt.Println("Error backing up Trivy DB:", err)
			return
		}
		if err := runTrivyUpdateCommand(*cacheDir); err != nil {
			fmt.Println(err)
			fmt.Println("Update failed, restoring from backup...")
			if restoreErr := restoreTrivyDB(*cacheDir); restoreErr != nil {
				fmt.Printf("Error restoring from backup: %v\n", restoreErr)
			}
			return
		}
		// Read the new metadata after update
		if newMetadata, err := readMetadata(metadataFile); err == nil {
			fmt.Printf("Next Trivy DB update will happen at: %s\n", newMetadata.NextUpdate.Format(time.RFC3339))
		}
		return
	}

	if time.Now().After(metadata.NextUpdate) {
		fmt.Println("Updating Trivy DB...")
		if err := backupTrivyDB(*cacheDir); err != nil {
			fmt.Println("Error backing up Trivy DB:", err)
			return
		}
		if err := runTrivyUpdateCommand(*cacheDir); err != nil {
			fmt.Println(err)
			fmt.Println("Update failed, restoring from backup...")
			if restoreErr := restoreTrivyDB(*cacheDir); restoreErr != nil {
				fmt.Printf("Error restoring from backup: %v\n", restoreErr)
			}
			return
		}
		fmt.Println("Trivy DB update complete.")
		// Read the new metadata after update
		if newMetadata, err := readMetadata(metadataFile); err == nil {
			fmt.Printf("Next Trivy DB update will happen at: %s\n", newMetadata.NextUpdate.Format(time.RFC3339))
		}
	} else {
		fmt.Println("Trivy DB is up-to-date. No update needed.")
		fmt.Printf("Next Trivy DB update will happen at: %s\n", metadata.NextUpdate.Format(time.RFC3339))
	}
}
