package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goplus/llar/pkgs/mod/versions"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [module-id]",
	Short: "Initialize a new module",
	Long:  `Initialize creates a new versions.json file in the current directory.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	modID := args[0]

	versionsPath := filepath.Join(".", "versions.json")
	if _, err := os.Stat(versionsPath); err == nil {
		return fmt.Errorf("versions.json already exists")
	}

	v := &versions.Versions{
		ModuleID:     modID,
		Dependencies: make(map[string][]versions.Dependency),
	}

	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return fmt.Errorf("failed to marshal versions.json: %w", err)
	}

	if err := os.WriteFile(versionsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write versions.json: %w", err)
	}

	fmt.Printf("Initialized module %s\n", modID)
	return nil
}
