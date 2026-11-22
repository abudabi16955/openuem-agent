package selfservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anmitsu/go-shlex"
)

// Executor defines the interface for running package installations.
type Executor interface {
	Execute(pkg PackageItem, localPath string, userToken ...uintptr) (string, error)
}

// BuildMSIArgs constructs command line arguments for msiexec.
func BuildMSIArgs(msiPath string, customArgs string) []string {
	args := []string{"/i", msiPath, "/quiet", "/norestart"}
	if customArgs != "" {
		if parsed, err := shlex.Split(customArgs, true); err == nil && len(parsed) > 0 {
			args = append(args, parsed...)
		} else {
			args = append(args, strings.Fields(customArgs)...)
		}
	}
	return args
}

// BuildWingetArgs constructs command line arguments for winget.
func BuildWingetArgs(packageID string, customArgs string) []string {
	args := []string{
		"install",
		"--id", packageID,
		"--exact",
		"--silent",
		"--accept-source-agreements",
		"--accept-package-agreements",
	}
	if customArgs != "" {
		if parsed, err := shlex.Split(customArgs, true); err == nil && len(parsed) > 0 {
			args = append(args, parsed...)
		} else {
			args = append(args, strings.Fields(customArgs)...)
		}
	}
	return args
}

// BuildCustomArgs parses a string of silent arguments into an argument slice.
func BuildCustomArgs(customArgs string) []string {
	if customArgs == "" {
		return []string{}
	}
	if parsed, err := shlex.Split(customArgs, true); err == nil {
		return parsed
	}
	return strings.Fields(customArgs)
}

// LocateExecutableInDir scans a directory for the primary executable or installer file.
func LocateExecutableInDir(dir string) (string, error) {
	var preferredExe string
	var fallbackExe string

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".msi" {
			preferredExe = path
			return filepath.SkipAll
		}
		if ext == ".exe" || ext == ".bat" || ext == ".cmd" || ext == ".ps1" {
			if fallbackExe == "" {
				fallbackExe = path
			}
			// Prefer setup.exe, install.exe
			base := strings.ToLower(info.Name())
			if strings.Contains(base, "setup") || strings.Contains(base, "install") {
				preferredExe = path
				return filepath.SkipAll
			}
		}
		return nil
	})

	if preferredExe != "" {
		return preferredExe, nil
	}
	if fallbackExe != "" {
		return fallbackExe, nil
	}
	return "", fmt.Errorf("no installer executable found in directory %s", dir)
}
