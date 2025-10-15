// Package cmd - Modernized pq using native Podman quadlet commands
// This shows ONLY the overlapping functionality replacement

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Masterminds/log-go"
	"github.com/spf13/cobra"
)

// ==============================================================================
// OVERLAPPING FUNCTIONALITY - Use Native Podman Commands
// ==============================================================================

// 1. INSTALL - Replace file copying with `podman quadlet install`
var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a quadlet from a quadlet repo",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		quadletName := args[0]
		
		// Step 1: Download from git (pq's unique feature - KEEP THIS)
		tmpDir, err := os.MkdirTemp("", "pq")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		
		log.Infof("Downloading quadlet %q from %s", quadletName, repoURL)
		downloadPath, err := downloadFromGit(repoURL, tmpDir)
		if err != nil {
			return err
		}
		
		quadletPath := filepath.Join(downloadPath, quadletName)
		
		// Step 2: REPLACED - Use native `podman quadlet install`
		// OLD: copyDir(quadletPath, filepath.Join(installDir, quadletName))
		// NEW: Use Podman's native install
		if err := installWithPodman(quadletPath); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		
		// Step 3: Start services (still using systemctl - no Podman equivalent)
		return startQuadletServices(quadletName)
	},
}

// installWithPodman uses native `podman quadlet install` command
func installWithPodman(quadletPath string) error {
	log.Info("Installing with native Podman quadlet commands")
	
	// Check if it's a directory (application) or single file
	info, err := os.Stat(quadletPath)
	if err != nil {
		return err
	}
	
	// Build command
	args := []string{"quadlet", "install"}
	
	// Add --user flag for rootless
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	// Auto-reload systemd (Podman does this by default)
	// Add path
	args = append(args, quadletPath)
	
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman quadlet install failed: %s\n%s", err, output)
	}
	
	log.Info(string(output))
	return nil
}

// 2. LIST - Replace manual directory walking with `podman quadlet list`
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the available quadlets",
	RunE: func(cmd *cobra.Command, args []string) error {
		if installed {
			// REPLACED - Use native `podman quadlet list`
			// OLD: Manual directory walking with quadlet.ListQuadlets()
			// NEW: Use Podman's native list
			return listInstalledWithPodman()
		}
		
		// List from repo (pq's unique feature - KEEP THIS)
		return listFromRepo(repoURL)
	},
}

// listInstalledWithPodman uses native `podman quadlet list` command
func listInstalledWithPodman() error {
	log.Info("Listing installed quadlets with native Podman")
	
	args := []string{"quadlet", "list"}
	
	// Add --user flag for rootless
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	// Use table format for cleaner output
	args = append(args, "--format", "table")
	
	cmd := exec.Command("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}

// 3. REMOVE - Replace manual file deletion with `podman quadlet rm`
var removeCmd = &cobra.Command{
	Use:     "remove",
	Short:   "Remove a quadlet",
	Aliases: []string{"uninstall", "rm"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		quadletName := args[0]
		
		// REPLACED - Use native `podman quadlet rm`
		// OLD: Manual os.RemoveAll() + systemd.DaemonReload()
		// NEW: Use Podman's native remove
		return removeWithPodman(quadletName)
	},
}

// removeWithPodman uses native `podman quadlet rm` command
func removeWithPodman(quadletName string) error {
	log.Infof("Removing quadlet %q with native Podman", quadletName)
	
	// Ensure .container extension
	if !strings.HasSuffix(quadletName, ".container") {
		quadletName += ".container"
	}
	
	args := []string{"quadlet", "rm"}
	
	// Add --user flag for rootless
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	// Add quadlet name
	args = append(args, quadletName)
	
	// Note: podman quadlet rm automatically reloads systemd
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("podman quadlet rm failed: %s\n%s", err, output)
	}
	
	log.Info(string(output))
	return nil
}

// 4. INSPECT - Replace file reading with `podman quadlet print`
var inspectCmd = &cobra.Command{
	Use:     "inspect",
	Short:   "Inspect a quadlet definition",
	Aliases: []string{"show", "print"},
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		quadletName := args[0]
		
		if fromInstalled {
			// REPLACED - Use native `podman quadlet print`
			// OLD: Manual file reading with outputQuadlet()
			// NEW: Use Podman's native print
			return inspectInstalledWithPodman(quadletName)
		}
		
		// Inspect from repo (pq's unique feature - KEEP THIS)
		return inspectFromRepo(repoURL, quadletName)
	},
}

// inspectInstalledWithPodman uses native `podman quadlet print` command
func inspectInstalledWithPodman(quadletName string) error {
	log.Infof("Inspecting installed quadlet %q with native Podman", quadletName)
	
	// Ensure .container extension
	if !strings.HasSuffix(quadletName, ".container") {
		quadletName += ".container"
	}
	
	args := []string{"quadlet", "print"}
	
	// Add --user flag for rootless
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	// Add quadlet name
	args = append(args, quadletName)
	
	cmd := exec.Command("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	return cmd.Run()
}

// ==============================================================================
// VALIDATION - New functionality using Podman (no previous pq equivalent)
// ==============================================================================

// 5. VALIDATE - NEW: Add validation before install
func validateQuadletWithPodman(quadletPath string) error {
	log.Info("Validating quadlet with native Podman")
	
	// Note: As of Podman 5.x, there isn't a direct `podman quadlet validate`
	// but we can use `podman quadlet install --dry-run` (if available)
	// OR we can try installing to a temp location and check for errors
	
	// For now, we'll use a test install approach
	args := []string{"quadlet", "install", "--help"}
	cmd := exec.Command("podman", args...)
	helpOutput, _ := cmd.CombinedOutput()
	
	// Check if --dry-run is supported
	if strings.Contains(string(helpOutput), "--dry-run") {
		return validateWithDryRun(quadletPath)
	}
	
	// Fallback: Try to parse the quadlet file ourselves
	return validateQuadletSyntax(quadletPath)
}

func validateWithDryRun(quadletPath string) error {
	args := []string{"quadlet", "install", "--dry-run"}
	
	if os.Geteuid() != 0 {
		args = append(args, "--user")
	}
	
	args = append(args, quadletPath)
	
	cmd := exec.Command("podman", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validation failed: %s\n%s", err, output)
	}
	
	log.Debug("Validation successful")
	return nil
}

func validateQuadletSyntax(quadletPath string) error {
	// Basic syntax validation
	// Check if required sections exist
	content, err := os.ReadFile(quadletPath)
	if err != nil {
		return err
	}
	
	text := string(content)
	
	// Check for required sections based on file type
	ext := filepath.Ext(quadletPath)
	switch ext {
	case ".container":
		if !strings.Contains(text, "[Container]") {
			return fmt.Errorf("missing required [Container] section")
		}
	case ".volume":
		if !strings.Contains(text, "[Volume]") {
			return fmt.Errorf("missing required [Volume] section")
		}
	case ".network":
		if !strings.Contains(text, "[Network]") {
			return fmt.Errorf("missing required [Network] section")
		}
	}
	
	log.Debug("Basic syntax validation passed")
	return nil
}

// ==============================================================================
// COMPARISON TABLE
// ==============================================================================

/*
OPERATION                 | OLD pq METHOD                    | NEW NATIVE PODMAN
--------------------------|----------------------------------|---------------------------
Install quadlet           | copyDir() + systemctl           | podman quadlet install
List installed            | Manual directory walk            | podman quadlet list
Remove quadlet            | os.RemoveAll() + daemon-reload  | podman quadlet rm
Inspect installed         | Manual file read                 | podman quadlet print
Validate quadlet          | (not implemented)                | podman quadlet install --dry-run
Generate from container   | (not implemented)                | podman generate systemd --format=quadlet

KEPT FROM OLD pq:
- Git repo discovery and cloning
- Repository listing (list from remote)
- Inspect from repo before install
- Package management concept

STILL NEEDED:
- systemctl start/stop (for service lifecycle)
- Git operations (for repo-based discovery)
*/

// ==============================================================================
// FEATURE DETECTION
// ==============================================================================

func hasPodmanQuadletCommands() bool {
	cmd := exec.Command("podman", "quadlet", "list", "--help")
	return cmd.Run() == nil
}

func detectPodmanVersion() (string, error) {
	cmd := exec.Command("podman", "version", "--format", "{{.Client.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// ==============================================================================
// MODIFIED INSTALL WITH VALIDATION
// ==============================================================================

var modernInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a quadlet from a quadlet repo (modern implementation)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		quadletName := args[0]
		
		// Check Podman version
		version, err := detectPodmanVersion()
		if err != nil {
			return fmt.Errorf("Podman not found: %w", err)
		}
		log.Infof("Using Podman version: %s", version)
		
		// Download from git (pq's unique value)
		tmpDir, err := os.MkdirTemp("", "pq")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		
		log.Infof("Downloading quadlet %q from %s", quadletName, repoURL)
		downloadPath, err := downloadFromGit(repoURL, tmpDir)
		if err != nil {
			return err
		}
		
		quadletPath := filepath.Join(downloadPath, quadletName)
		
		// NEW: Validate before installing
		log.Info("Validating quadlet...")
		if err := validateQuadletWithPodman(quadletPath); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
		
		// NEW: Install with native Podman
		if hasPodmanQuadletCommands() {
			log.Info("Using native Podman quadlet commands")
			if err := installWithPodman(quadletPath); err != nil {
				return err
			}
		} else {
			log.Warn("Podman quadlet commands not available, falling back to manual install")
			// Fallback to old method
			if err := copyDir(quadletPath, filepath.Join(installDir, quadletName)); err != nil {
				return err
			}
			// Manual systemd reload
			if err := systemdDaemonReload(); err != nil {
				return err
			}
		}
		
		// Start services (still using systemctl)
		return startQuadletServices(quadletName)
	},
}
