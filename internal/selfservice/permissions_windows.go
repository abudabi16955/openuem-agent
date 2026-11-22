//go:build windows

package selfservice

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// SetStrictSystemAdminPermissions configures ACL so that SYSTEM and Administrators
// have Full Control, while Authenticated Users have only Read access.
func SetStrictSystemAdminPermissions(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FR;;;AU)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

// SetCacheExclusivePermissions configures ACL so that ONLY SYSTEM and Administrators
// have access (no read/write for standard Users) to prevent TOCTOU file tampering.
func SetCacheExclusivePermissions(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

// GrantUserReadAndExecute configures ACL so that SYSTEM and Administrators retain Full Control,
// while the specified user SID is granted Read and Execute access (S-14).
func GrantUserReadAndExecute(path string, userSID string) error {
	if userSID == "" {
		return fmt.Errorf("user SID cannot be empty")
	}
	sddl := fmt.Sprintf("D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;GRGX;;;%s)", userSID)
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("failed to parse security descriptor for user SID %s: %w", userSID, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}
