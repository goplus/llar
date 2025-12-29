package internal

// TODO: support

// var buildVerbose bool

// var buildCmd = &cobra.Command{
// 	Use:   "build [directory]",
// 	Short: "Build the current module",
// 	Long: `Build compiles the current module and its dependencies.
// If directory is specified, looks for versions.json and formula in that directory.`,
// 	Args: cobra.MaximumNArgs(1),
// 	RunE: runBuild,
// }

// func init() {
// 	buildCmd.Flags().BoolVarP(&buildVerbose, "verbose", "v", false, "Enable verbose build output")
// 	rootCmd.AddCommand(buildCmd)
// }

// func runBuild(cmd *cobra.Command, args []string) error {
// 	dir := "."
// 	if len(args) > 0 {
// 		dir = args[0]
// 	}

// 	versionsPath := filepath.Join(dir, "versions.json")
// 	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
// 		return fmt.Errorf("versions.json not found in %s", dir)
// 	}

// 	v, err := versions.Parse(versionsPath, nil)
// 	if err != nil {
// 		return fmt.Errorf("failed to parse versions.json: %w", err)
// 	}

// 	// Get the current version from the first entry in deps, or use "latest"
// 	var currentVersion string
// 	for ver := range v.Dependencies {
// 		currentVersion = ver
// 		break
// 	}

// 	ctx := context.Background()

// 	builder := build.NewBuilder()
// 	if err := builder.Init(ctx, vcs.NewGitVCS(), "https://github.com/MeteorsLiu/llarmvp-formula"); err != nil {
// 		return fmt.Errorf("failed to init builder: %w", err)
// 	}

// 	// Load packages using modload
// 	formulas, err := modload.LoadPackages(ctx, module.Version{ID: v.ModuleID, Version: currentVersion}, modload.PackageOpts{
// 		LocalDir: dir,
// 	})
// 	if err != nil {
// 		return fmt.Errorf("failed to load packages: %w", err)
// 	}

// 	// Convert formulas to build targets
// 	targets := make([]build.BuildTarget, len(formulas))
// 	for i, f := range formulas {
// 		targets[i] = build.BuildTarget{
// 			Version: f.Version,
// 			Dir:     f.Dir,
// 			Project: f.Proj,
// 			OnBuild: f.OnBuild,
// 		}
// 	}

// 	matrix := formula.Matrix{
// 		Require: map[string][]string{
// 			"os":   {runtime.GOOS},
// 			"arch": {runtime.GOARCH},
// 		},
// 	}

// 	// Handle verbose output
// 	var savedStdout, savedStderr *os.File
// 	if !buildVerbose {
// 		// Redirect stdout/stderr for formulas
// 		for i := range formulas {
// 			formulas[i].SetStdout(io.Discard)
// 			formulas[i].SetStderr(io.Discard)
// 		}

// 		// Also redirect os.Stdout/os.Stderr
// 		savedStdout = os.Stdout
// 		savedStderr = os.Stderr
// 		devNull, err := os.Open(os.DevNull)
// 		if err != nil {
// 			return fmt.Errorf("failed to open devnull: %w", err)
// 		}
// 		os.Stdout = devNull
// 		os.Stderr = devNull
// 		defer func() {
// 			devNull.Close()
// 			os.Stdout = savedStdout
// 			os.Stderr = savedStderr
// 		}()
// 	}

// 	mainModule := module.Version{ID: v.ModuleID, Version: currentVersion}
// 	if err := builder.Build(ctx, mainModule, targets, matrix); err != nil {
// 		return fmt.Errorf("failed to build: %w", err)
// 	}

// 	// Restore stdout/stderr before printing pkgconfig info
// 	if !buildVerbose {
// 		os.Stdout = savedStdout
// 		os.Stderr = savedStderr
// 	}

// 	// Print pkgconfig info for main module (first in formulas)
// 	if len(formulas) > 0 {
// 		main := formulas[0]
// 		printPkgConfigInfo(main.ID, main.Version.Version, matrix)
// 	}

// 	return nil
// }
