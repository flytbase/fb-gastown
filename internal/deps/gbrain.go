package deps

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/state"
)

const (
	GBrainVersion = "0.42.65.0"
	BunVersion    = "1.3.14"
	gbrainCommit  = "1f319e6d5aff7674d8f48f289768ff75911a9ea8"
)

type GBrainInstallResult struct {
	Path      string
	Version   string
	Installed bool
	Managed   bool
}

func ManagedGBrainRoot() string {
	return filepath.Join(state.CacheDir(), "extensions", "gbrain", GBrainVersion)
}

func ManagedGBrainPath() string {
	return filepath.Join(ManagedGBrainRoot(), "node_modules", ".bin", "gbrain")
}

func FindGBrain() string {
	candidates := []string{ManagedGBrainPath()}
	if path, err := exec.LookPath("gbrain"); err == nil {
		candidates = append(candidates, path)
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".bun", "bin", "gbrain"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path
		}
	}
	return ""
}

// EnsureGBrain returns a compatible GBrain command. Existing installations are
// reused; first-time users get an isolated extension under Gas Town's cache.
func EnsureGBrain() (GBrainInstallResult, error) {
	if path := ManagedGBrainPath(); path != "" {
		version, err := gbrainVersion(path)
		if err == nil && version == GBrainVersion {
			return GBrainInstallResult{
				Path:    path,
				Version: version,
				Managed: strings.HasPrefix(path, ManagedGBrainRoot()+string(filepath.Separator)),
			}, nil
		}
	}

	root := ManagedGBrainRoot()
	if err := os.MkdirAll(root, 0755); err != nil {
		return GBrainInstallResult{}, fmt.Errorf("creating managed GBrain extension directory: %w", err)
	}
	bunPath, err := ensureManagedBun()
	if err != nil {
		return GBrainInstallResult{}, err
	}
	spec := "github:garrytan/gbrain#" + gbrainCommit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bunPath, "add", "--cwd", root, "--exact", spec)
	cmd.Env = prependPath(cmd.Environ(), filepath.Dir(bunPath))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return GBrainInstallResult{}, fmt.Errorf("installing managed GBrain %s: %w\n%s", GBrainVersion, err, strings.TrimSpace(string(output)))
	}
	path := ManagedGBrainPath()
	version, err := gbrainVersionWithPath(path, filepath.Dir(bunPath))
	if err != nil {
		return GBrainInstallResult{}, fmt.Errorf("verifying managed GBrain: %w", err)
	}
	return GBrainInstallResult{Path: path, Version: version, Installed: true, Managed: true}, nil
}

func ensureManagedBun() (string, error) {
	root := filepath.Join(state.CacheDir(), "extensions", "bun")
	path := filepath.Join(root, "bin", "bun")
	if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
		if version, versionErr := bunVersion(path); versionErr == nil && version == BunVersion {
			return path, nil
		}
	}
	if global, err := exec.LookPath("bun"); err == nil {
		if version, versionErr := bunVersion(global); versionErr == nil && version == BunVersion {
			if err := copyExecutable(global, path); err != nil {
				return "", err
			}
			return path, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		global := filepath.Join(home, ".bun", "bin", "bun")
		if info, statErr := os.Stat(global); statErr == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			if version, versionErr := bunVersion(global); versionErr == nil && version == BunVersion {
				if err := copyExecutable(global, path); err != nil {
					return "", err
				}
				return path, nil
			}
		}
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", fmt.Errorf("creating managed Bun directory: %w", err)
	}
	script, err := os.CreateTemp("", "gastown-bun-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("creating Bun installer file: %w", err)
	}
	scriptPath := script.Name()
	script.Close()
	defer os.Remove(scriptPath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	download := exec.CommandContext(ctx, "curl", "-fsSL", "https://bun.sh/install", "-o", scriptPath)
	if output, err := download.CombinedOutput(); err != nil {
		return "", fmt.Errorf("downloading Bun for the GBrain extension: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	install := exec.CommandContext(ctx, "bash", scriptPath, "bun-v"+BunVersion)
	install.Env = append(install.Environ(), "BUN_INSTALL="+root)
	if output, err := install.CombinedOutput(); err != nil {
		return "", fmt.Errorf("installing managed Bun: %w\n%s", err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("managed Bun installer did not create %s", path)
	}
	version, err := bunVersion(path)
	if err != nil {
		return "", fmt.Errorf("verifying managed Bun: %w", err)
	}
	if version != BunVersion {
		return "", fmt.Errorf("managed Bun version %s does not match required %s", version, BunVersion)
	}
	return path, nil
}

func bunVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("opening Bun bootstrap binary: %w", err)
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("creating managed Bun bin directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(target), "bun.tmp.*")
	if err != nil {
		return fmt.Errorf("creating managed Bun binary: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, input); err != nil {
		temp.Close()
		return fmt.Errorf("copying managed Bun binary: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing managed Bun binary: %w", err)
	}
	if err := os.Chmod(tempPath, 0700); err != nil {
		return fmt.Errorf("setting managed Bun permissions: %w", err)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("installing managed Bun binary: %w", err)
	}
	return nil
}

func gbrainVersion(path string) (string, error) {
	binDirs := filepath.Join(state.CacheDir(), "extensions", "bun", "bin")
	if home, err := os.UserHomeDir(); err == nil {
		binDirs += string(os.PathListSeparator) + filepath.Join(home, ".bun", "bin")
	}
	return gbrainVersionWithPath(path, binDirs)
}

func gbrainVersionWithPath(path, binDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	if binDir != "" {
		cmd.Env = prependPath(cmd.Environ(), binDir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w: %s", path, err, strings.TrimSpace(string(output)))
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 {
		return "", fmt.Errorf("unrecognized GBrain version output: %q", strings.TrimSpace(string(output)))
	}
	return fields[len(fields)-1], nil
}

func prependPath(env []string, dir string) []string {
	for i, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			env[i] = "PATH=" + dir + string(os.PathListSeparator) + strings.TrimPrefix(item, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+dir)
}
