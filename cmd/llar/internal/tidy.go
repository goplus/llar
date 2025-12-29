package internal

// TODO: support

// var tidyCmd = &cobra.Command{
// 	Use:   "tidy",
// 	Short: "Tidy the module dependencies using MVS",
// 	Long:  `Tidy resolves all dependencies using Minimal Version Selection (MVS) algorithm and updates versions.json.`,
// 	RunE:  runTidy,
// }

// func init() {
// 	rootCmd.AddCommand(tidyCmd)
// }

// func runTidy(cmd *cobra.Command, args []string) error {
// 	versionsPath := filepath.Join(".", "llar.mod")
// 	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
// 		return fmt.Errorf("versions.json not found, run 'llar init' first")
// 	}

// 	v, err := versions.Parse(versionsPath, nil)
// 	if err != nil {
// 		return fmt.Errorf("failed to parse versions.json: %w", err)
// 	}

// 	// Get the current version
// 	var currentVersion string
// 	for ver := range v.Dependencies {
// 		currentVersion = ver
// 		break
// 	}
// 	if currentVersion == "" {
// 		fmt.Println("No dependencies to tidy")
// 		return nil
// 	}

// 	ctx := context.Background()

// 	mainMod := module.Version{
// 		ID:      v.ModuleID,
// 		Version: currentVersion,
// 	}

// 	// LoadPackages with Tidy: true will compute minimal dependencies
// 	// using mvs.Req and update versions.json automatically
// 	_, err = modload.LoadPackages(ctx, mainMod, modload.PackageOpts{Tidy: true})
// 	if err != nil {
// 		return fmt.Errorf("failed to tidy dependencies: %w", err)
// 	}

// 	fmt.Printf("Tidied dependencies for %s@%s\n", v.ModuleID, currentVersion)
// 	return nil
// }
