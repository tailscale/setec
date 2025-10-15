// Package main - leger: Podman Quadlet Manager with Secrets Integration
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/yourorg/leger/internal/cli"
	"github.com/yourorg/leger/internal/daemon"
	"github.com/yourorg/leger/internal/podman"
	"github.com/yourorg/leger/pkg/types"
)

var (
	version   = "development"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "leger",
		Short: "Podman Quadlet Manager with Secrets Integration",
		Long: `leger manages Podman Quadlets with Git-based installation, 
staged updates, backup/restore, and Setec secrets integration.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, buildDate),
	}

	// CLI commands
	rootCmd.AddCommand(cli.InstallCmd())
	rootCmd.AddCommand(cli.ListCmd())
	rootCmd.AddCommand(cli.RemoveCmd())
	rootCmd.AddCommand(cli.InspectCmd())
	
	// Staged updates
	rootCmd.AddCommand(cli.StageCmd())
	rootCmd.AddCommand(cli.StagedCmd())
	rootCmd.AddCommand(cli.DiffCmd())
	rootCmd.AddCommand(cli.ApplyCmd())
	rootCmd.AddCommand(cli.DiscardCmd())
	
	// Backup & restore
	rootCmd.AddCommand(cli.BackupCmd())
	rootCmd.AddCommand(cli.BackupsCmd())
	rootCmd.AddCommand(cli.RestoreCmd())
	
	// Validation
	rootCmd.AddCommand(cli.ValidateCmd())
	rootCmd.AddCommand(cli.CheckConflictsCmd())
	
	// Service management
	rootCmd.AddCommand(cli.StatusCmd())
	rootCmd.AddCommand(cli.LogsCmd())
	rootCmd.AddCommand(cli.StartCmd())
	rootCmd.AddCommand(cli.StopCmd())
	rootCmd.AddCommand(cli.RestartCmd())
	
	// Daemon mode
	rootCmd.AddCommand(daemon.DaemonCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// ===================================================================
// internal/cli/install.go
// ===================================================================
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yourorg/leger/internal/git"
	"github.com/yourorg/leger/internal/podman"
	"github.com/yourorg/leger/internal/validation"
	"github.com/yourorg/leger/pkg/types"
)

func InstallCmd() *cobra.Command {
	var (
		scope  string
		branch string
	)

	cmd := &cobra.Command{
		Use:   "install <repo-url>",
		Short: "Install a quadlet from a Git repository",
		Long: `Install a Podman Quadlet from a Git repository.
		
Examples:
  leger install https://github.com/org/repo/tree/main/nginx
  leger install https://github.com/org/repo/tree/main/nginx --scope=system
  leger install https://github.com/org/repo/tree/dev/nginx --branch=dev`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoURL := args[0]
			
			// 1. Parse Git URL
			gitRepo, err := git.ParseURL(repoURL, branch)
			if err != nil {
				return fmt.Errorf("invalid Git URL: %w", err)
			}
			
			// 2. Clone to temp directory
			tmpDir, err := os.MkdirTemp("", "leger-")
			if err != nil {
				return fmt.Errorf("create temp dir: %w", err)
			}
			defer os.RemoveAll(tmpDir)
			
			fmt.Printf("Cloning %s...\n", gitRepo.URL)
			quadletPath, err := git.CloneQuadlet(gitRepo, tmpDir)
			if err != nil {
				return fmt.Errorf("clone failed: %w", err)
			}
			
			// 3. Load quadlet metadata
			metadata, err := types.LoadMetadata(quadletPath)
			if err != nil {
				return fmt.Errorf("load metadata: %w", err)
			}
			
			// 4. Validate quadlet
			fmt.Println("Validating quadlet...")
			if err := validation.ValidateQuadlet(quadletPath, metadata); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}
			
			// 5. Check for conflicts
			fmt.Println("Checking for conflicts...")
			if conflicts := validation.CheckConflicts(metadata); len(conflicts) > 0 {
				fmt.Println("⚠️  Conflicts detected:")
				for _, c := range conflicts {
					fmt.Printf("  - %s\n", c)
				}
				return fmt.Errorf("conflicts must be resolved before install")
			}
			
			// 6. Check if secrets are needed
			if len(metadata.Secrets) > 0 {
				fmt.Println("📋 Secrets required:")
				for _, s := range metadata.Secrets {
					fmt.Printf("  - %s\n", s.Name)
				}
				
				// Verify leger-daemon is running
				if !daemon.IsRunning() {
					return fmt.Errorf("leger-daemon must be running for secret injection. Run: sudo systemctl start leger-daemon")
				}
				
				// Request daemon to prepare secrets
				if err := daemon.PrepareSecrets(metadata.Secrets); err != nil {
					return fmt.Errorf("prepare secrets: %w", err)
				}
				fmt.Println("✓ Secrets prepared")
			}
			
			// 7. Install using native Podman
			fmt.Printf("Installing %s...\n", metadata.Name)
			if err := podman.Install(quadletPath, scope); err != nil {
				return fmt.Errorf("install failed: %w", err)
			}
			
			// 8. Start services
			fmt.Println("Starting services...")
			services, err := podman.GetServices(metadata.Name, scope)
			if err != nil {
				return fmt.Errorf("get services: %w", err)
			}
			
			for _, svc := range services {
				if err := podman.StartService(svc, scope); err != nil {
					return fmt.Errorf("start service %s: %w", svc, err)
				}
				fmt.Printf("✓ Started %s\n", svc)
			}
			
			fmt.Printf("\n✅ Successfully installed %s\n", metadata.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "user", "Installation scope: user or system")
	cmd.Flags().StringVar(&branch, "branch", "main", "Git branch to use")
	
	return cmd
}

// ===================================================================
// internal/podman/quadlet.go - Native Podman Integration
// ===================================================================
package podman

import (
	"fmt"
	"os"
	"os/exec"
)

// Install installs a quadlet using native `podman quadlet install`
func Install(quadletPath string, scope string) error {
	args := []string{"quadlet", "install"}
	
	if scope == "user" || os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	args = append(args, quadletPath)
	
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman quadlet install: %w\n%s", err, output)
	}
	
	return nil
}

// List lists installed quadlets using native `podman quadlet list`
func List(scope string) ([]QuadletInfo, error) {
	args := []string{"quadlet", "list"}
	
	if scope == "user" || os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	args = append(args, "--format", "json")
	
	cmd := exec.Command("podman", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("podman quadlet list: %w", err)
	}
	
	// Parse JSON output
	var quadlets []QuadletInfo
	if err := json.Unmarshal(output, &quadlets); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	
	return quadlets, nil
}

// Remove removes a quadlet using native `podman quadlet rm`
func Remove(name string, scope string) error {
	args := []string{"quadlet", "rm"}
	
	if scope == "user" || os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	args = append(args, name)
	
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman quadlet rm: %w\n%s", err, output)
	}
	
	return nil
}

// ===================================================================
// internal/daemon/setec.go - Setec Integration
// ===================================================================
package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/tailscale/setec/client/setec"
	"github.com/yourorg/leger/internal/podman"
	"github.com/yourorg/leger/pkg/types"
)

type SetecDaemon struct {
	ctx          context.Context
	client       *setec.Client
	store        *setec.Store
	pollInterval time.Duration
	secretPrefix string
}

func NewSetecDaemon(ctx context.Context, config DaemonConfig) (*SetecDaemon, error) {
	// Setec client uses Tailscale authentication automatically
	client := &setec.Client{
		Server: config.SetecServer,
	}
	
	return &SetecDaemon{
		ctx:          ctx,
		client:       client,
		pollInterval: config.PollInterval,
		secretPrefix: config.SecretPrefix,
	}, nil
}

func (sd *SetecDaemon) Start() error {
	// 1. Discover quadlets that need secrets
	quadlets, err := sd.discoverQuadletsWithSecrets()
	if err != nil {
		return fmt.Errorf("discover quadlets: %w", err)
	}
	
	// 2. Extract all secret names
	secretNames := make([]string, 0)
	for _, q := range quadlets {
		for _, s := range q.Secrets {
			secretNames = append(secretNames, s.Name)
		}
	}
	
	if len(secretNames) == 0 {
		log.Info("No secrets to manage")
		return nil
	}
	
	// 3. Create Setec store for all secrets
	log.Infof("Initializing Setec store for %d secrets", len(secretNames))
	sd.store, err = setec.NewStore(sd.ctx, setec.StoreConfig{
		Client:       sd.client,
		Secrets:      secretNames,
		PollInterval: sd.pollInterval,
	})
	if err != nil {
		return fmt.Errorf("create setec store: %w", err)
	}
	
	// 4. Initial sync to Podman secrets
	if err := sd.syncSecretsToPodman(quadlets); err != nil {
		return fmt.Errorf("initial sync: %w", err)
	}
	
	// 5. Watch for updates
	go sd.watchSecretUpdates(quadlets)
	
	log.Info("Setec daemon started successfully")
	return nil
}

func (sd *SetecDaemon) syncSecretsToPodman(quadlets []types.QuadletMetadata) error {
	for _, q := range quadlets {
		for _, secretDef := range q.Secrets {
			// Get secret value from Setec store
			secret := sd.store.Secret(secretDef.Name)
			if secret == nil {
				log.Warnf("Secret %s not found in store", secretDef.Name)
				continue
			}
			
			// Create or update Podman secret
			if err := podman.CreateOrUpdateSecret(
				secretDef.PodmanSecret,
				secret.Get(),
				q.Scope,
			); err != nil {
				return fmt.Errorf("sync secret %s: %w", secretDef.Name, err)
			}
			
			log.Debugf("✓ Synced %s → %s", secretDef.Name, secretDef.PodmanSecret)
		}
	}
	return nil
}

func (sd *SetecDaemon) watchSecretUpdates(quadlets []types.QuadletMetadata) {
	ticker := time.NewTicker(sd.pollInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-sd.ctx.Done():
			return
		case <-ticker.C:
			// Setec store auto-updates in background
			// We just need to sync any changes to Podman
			if err := sd.syncSecretsToPodman(quadlets); err != nil {
				log.Errorf("Secret sync failed: %v", err)
			} else {
				log.Debug("Secret sync completed")
			}
		}
	}
}

func (sd *SetecDaemon) discoverQuadletsWithSecrets() ([]types.QuadletMetadata, error) {
	// Discover all installed quadlets
	userQuadlets, _ := podman.List("user")
	systemQuadlets, _ := podman.List("system")
	
	quadlets := make([]types.QuadletMetadata, 0)
	
	// Load metadata for each quadlet
	for _, q := range append(userQuadlets, systemQuadlets...) {
		metadata, err := types.LoadMetadata(q.Path)
		if err != nil {
			log.Warnf("Failed to load metadata for %s: %v", q.Name, err)
			continue
		}
		
		if len(metadata.Secrets) > 0 {
			quadlets = append(quadlets, metadata)
		}
	}
	
	return quadlets, nil
}

// ===================================================================
// internal/podman/secrets.go - Podman Secrets API
// ===================================================================
package podman

import (
	"bytes"
	"fmt"
	"os/exec"
)

// CreateOrUpdateSecret creates or updates a Podman secret
func CreateOrUpdateSecret(name string, value []byte, scope string) error {
	// Check if secret exists
	exists, err := secretExists(name, scope)
	if err != nil {
		return err
	}
	
	if exists {
		// Remove old secret
		if err := removeSecret(name, scope); err != nil {
			return fmt.Errorf("remove old secret: %w", err)
		}
	}
	
	// Create new secret
	args := []string{"secret", "create"}
	
	if scope == "user" {
		args = append(args, "--user")
	}
	
	args = append(args, name, "-")
	
	cmd := exec.Command("podman", args...)
	cmd.Stdin = bytes.NewReader(value)
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman secret create: %w\n%s", err, output)
	}
	
	return nil
}

func secretExists(name string, scope string) (bool, error) {
	args := []string{"secret", "inspect", name}
	
	if scope == "user" {
		args = append(args, "--user")
	}
	
	cmd := exec.Command("podman", args...)
	err := cmd.Run()
	
	if err != nil {
		// Secret doesn't exist
		return false, nil
	}
	
	return true, nil
}

func removeSecret(name string, scope string) error {
	args := []string{"secret", "rm", name}
	
	if scope == "user" {
		args = append(args, "--user")
	}
	
	cmd := exec.Command("podman", args...)
	return cmd.Run()
}

// ===================================================================
// internal/staging/manager.go - Staged Updates
// ===================================================================
package staging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yourorg/leger/internal/git"
	"github.com/yourorg/leger/internal/validation"
	"github.com/yourorg/leger/pkg/types"
)

type StagingManager struct {
	stagingDir string
	manifestDir string
}

func NewStagingManager(stagingDir, manifestDir string) *StagingManager {
	return &StagingManager{
		stagingDir:  stagingDir,
		manifestDir: manifestDir,
	}
}

func (sm *StagingManager) Stage(name string) error {
	// 1. Get quadlet's Git source
	metadata, err := types.GetQuadletMetadata(name)
	if err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}
	
	if metadata.GitSource == "" {
		return fmt.Errorf("quadlet %s has no Git source", name)
	}
	
	// 2. Clone latest version
	tmpDir, err := os.MkdirTemp("", "leger-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	
	fmt.Printf("Fetching latest version from %s...\n", metadata.GitSource)
	gitRepo, err := git.ParseURL(metadata.GitSource, metadata.GitBranch)
	if err != nil {
		return err
	}
	
	quadletPath, err := git.CloneQuadlet(gitRepo, tmpDir)
	if err != nil {
		return fmt.Errorf("clone: %w", err)
	}
	
	// 3. Validate
	fmt.Println("Validating...")
	newMetadata, err := types.LoadMetadata(quadletPath)
	if err != nil {
		return fmt.Errorf("load metadata: %w", err)
	}
	
	if err := validation.ValidateQuadlet(quadletPath, newMetadata); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	
	// 4. Stage in preview area
	stagePath := filepath.Join(sm.stagingDir, name)
	if err := os.RemoveAll(stagePath); err != nil {
		return err
	}
	
	if err := copyDir(quadletPath, stagePath); err != nil {
		return fmt.Errorf("stage files: %w", err)
	}
	
	// 5. Create staging manifest
	manifest := types.StagingManifest{
		Name:      name,
		Timestamp: time.Now(),
		OldVersion: metadata.Version,
		NewVersion: newMetadata.Version,
		GitCommit: newMetadata.GitCommit,
	}
	
	if err := sm.saveManifest(name, manifest); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	
	fmt.Printf("✓ Staged %s (%s → %s)\n", name, metadata.Version, newMetadata.Version)
	return nil
}

func (sm *StagingManager) Apply(name string) error {
	// Implementation similar to BlueBuild's apply logic
	// 1. Backup current
	// 2. Install staged version using podman.Install()
	// 3. Restart services
	// 4. Clean up staging
	return nil
}

// ===================================================================
// pkg/types/metadata.go - Quadlet Metadata
// ===================================================================
package types

import (
	"os"
	"path/filepath"
	
	"gopkg.in/yaml.v3"
)

type QuadletMetadata struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Version     string          `yaml:"version"`
	GitSource   string          `yaml:"git-source"`
	GitBranch   string          `yaml:"git-branch"`
	GitCommit   string          `yaml:"git-commit"`
	Scope       string          `yaml:"scope"`
	Secrets     []SecretMapping `yaml:"secrets"`
	Requires    []string        `yaml:"requires"`
	Ports       []string        `yaml:"ports"`
	Volumes     []string        `yaml:"volumes"`
}

type SecretMapping struct {
	Name         string `yaml:"name"`          // Setec secret name
	PodmanSecret string `yaml:"podman-secret"` // Podman secret name
	Env          string `yaml:"env"`           // Environment variable name
}

func LoadMetadata(quadletPath string) (QuadletMetadata, error) {
	metadataFile := filepath.Join(quadletPath, ".leger.yaml")
	
	data, err := os.ReadFile(metadataFile)
	if err != nil {
		return QuadletMetadata{}, err
	}
	
	var metadata QuadletMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return QuadletMetadata{}, err
	}
	
	return metadata, nil
}
