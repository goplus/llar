package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goplus/llar/pkgs/mod/versions"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get [module@version]",
	Short: "Add a dependency to versions.json",
	Long:  `Get adds a new dependency to the current module's versions.json file.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runGet,
}

var getVersion string

func init() {
	getCmd.Flags().StringVarP(&getVersion, "version", "v", "", "Version key in versions.json to add dependency to")
	rootCmd.AddCommand(getCmd)
}

func runGet(cmd *cobra.Command, args []string) error {
	depModID, depVersion := parseModuleArg(args[0])

	versionsPath := filepath.Join(".", "versions.json")
	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
		return fmt.Errorf("versions.json not found, run 'llar init' first")
	}

	v, err := versions.Parse(versionsPath, nil)
	if err != nil {
		return fmt.Errorf("failed to parse versions.json: %w", err)
	}

	// Initialize deps map if needed
	if v.Dependencies == nil {
		v.Dependencies = make(map[string][]versions.Dependency)
	}

	// Use getVersion as the key, default to empty string
	targetVersion := getVersion

	// Check if dependency already exists
	deps := v.Dependencies[targetVersion]
	for i, dep := range deps {
		if dep.ModuleID == depModID {
			// Update existing dependency version
			deps[i].Version = depVersion
			v.Dependencies[targetVersion] = deps
			goto write
		}
	}

	// Add new dependency
	v.Dependencies[targetVersion] = append(deps, versions.Dependency{
		ModuleID: depModID,
		Version:  depVersion,
	})

write:
	data, err := json.MarshalIndent(v, "", "\t")
	if err != nil {
		return fmt.Errorf("failed to marshal versions.json: %w", err)
	}

	if err := os.WriteFile(versionsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write versions.json: %w", err)
	}

	fmt.Printf("Added dependency %s@%s\n", depModID, depVersion)
	return nil
}
