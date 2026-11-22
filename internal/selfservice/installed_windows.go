//go:build windows

package selfservice

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	regUninstallKey         = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	regUninstallKey32       = `SOFTWARE\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
	regProfileListKey       = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`
)

// ScanRegistryInstalled scans Windows Uninstall registry keys (HKLM, HKCU, and Wow6432Node)
// returning a map of normalized application names / subkey names to their DisplayVersion.
func ScanRegistryInstalled() (map[string]string, error) {
	results := make(map[string]string)

	// 1. Scan HKLM 64-bit and 32-bit
	_ = scanUninstallHive(results, registry.LOCAL_MACHINE, regUninstallKey)
	_ = scanUninstallHive(results, registry.LOCAL_MACHINE, regUninstallKey32)

	// 2. Scan HKCU 64-bit and 32-bit
	_ = scanUninstallHive(results, registry.CURRENT_USER, regUninstallKey)
	_ = scanUninstallHive(results, registry.CURRENT_USER, regUninstallKey32)

	// 3. Scan HKEY_USERS for logged in users
	sids := getUserSIDsFromProfileList()
	for _, sid := range sids {
		_ = scanUninstallHive(results, registry.USERS, fmt.Sprintf(`%s\%s`, sid, regUninstallKey))
		_ = scanUninstallHive(results, registry.USERS, fmt.Sprintf(`%s\%s`, sid, regUninstallKey32))
	}

	return results, nil
}

func scanUninstallHive(results map[string]string, hive registry.Key, basePath string) error {
	k, err := registry.OpenKey(hive, basePath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return err
	}
	defer k.Close()

	subKeyNames, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}

	for _, name := range subKeyNames {
		sk, err := registry.OpenKey(hive, fmt.Sprintf(`%s\%s`, basePath, name), registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		dispName, _, errName := sk.GetStringValue("DisplayName")
		dispVer, _, errVer := sk.GetStringValue("DisplayVersion")
		sk.Close()

		if errVer != nil || strings.TrimSpace(dispVer) == "" {
			continue
		}

		ver := strings.TrimSpace(dispVer)

		// Record by subkey name (often GUID or Package ID)
		cleanKey := strings.ToLower(strings.TrimSpace(name))
		if cleanKey != "" && results[cleanKey] == "" {
			results[cleanKey] = ver
		}

		// Record by DisplayName
		if errName == nil {
			cleanName := strings.ToLower(strings.TrimSpace(dispName))
			if cleanName != "" && results[cleanName] == "" {
				results[cleanName] = ver
			}
		}
	}

	return nil
}

func getUserSIDsFromProfileList() []string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regProfileListKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var userSIDs []string
	for _, name := range names {
		// Filter out standard built-in service accounts (S-1-5-18, 19, 20) and keep real user SIDs
		if strings.HasPrefix(name, "S-1-5-21-") && !strings.HasSuffix(name, "_Classes") {
			userSIDs = append(userSIDs, name)
		}
	}
	return userSIDs
}
