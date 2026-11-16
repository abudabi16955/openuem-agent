//go:build windows

package bitlocker

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/doncicuto/comshim"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// GetBitLockerVolumes queries Win32_EncryptableVolume from Root\CIMv2\Security\MicrosoftVolumeEncryption using go-ole.
// If go-ole encounters an issue or returns no volumes, it attempts the PowerShell CIM fallback.
func GetBitLockerVolumes() ([]VolumeInfo, error) {
	vols, err := getBitLockerVolumesCOM()
	if err == nil && len(vols) > 0 {
		return vols, nil
	}
	if err != nil {
		log.Printf("[DEBUG]: BitLocker COM query error: %v, attempting PowerShell CIM fallback", err)
	}

	// Fallback to PowerShell CIM
	psVols, psErr := getBitLockerVolumesPowerShell()
	if psErr == nil && len(psVols) > 0 {
		return psVols, nil
	}

	if err != nil {
		return nil, err
	}
	return vols, nil
}

// GetRecoveryKeys retrieves all Numerical Password (type 3) recovery keys from all protected volumes.
func GetRecoveryKeys() ([]KeyProtector, error) {
	vols, err := GetBitLockerVolumes()
	if err != nil {
		return nil, err
	}

	var allProtectors []KeyProtector
	for _, v := range vols {
		allProtectors = append(allProtectors, v.Protectors...)
	}
	return allProtectors, nil
}

func getBitLockerVolumesCOM() ([]VolumeInfo, error) {
	if err := comshim.Add(1); err != nil {
		return nil, fmt.Errorf("could not initialize COM thread: %w", err)
	}
	defer func() {
		if err := comshim.Done(); err != nil {
			log.Printf("[ERROR]: COM Done error: %v", err)
		}
	}()

	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return nil, fmt.Errorf("failed to create SWbemLocator: %w", err)
	}
	defer unknown.Release()

	locator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil, fmt.Errorf("failed to query IDispatch from SWbemLocator: %w", err)
	}
	defer locator.Release()

	// Connect to Root\CIMv2\Security\MicrosoftVolumeEncryption
	serviceVariant, err := oleutil.CallMethod(locator, "ConnectServer", nil, `root\cimv2\Security\MicrosoftVolumeEncryption`)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MicrosoftVolumeEncryption namespace: %w", err)
	}
	service := serviceVariant.ToIDispatch()
	defer service.Release()

	volumeColVariant, err := oleutil.CallMethod(service, "ExecQuery", "SELECT * FROM Win32_EncryptableVolume")
	if err != nil {
		return nil, fmt.Errorf("failed to query Win32_EncryptableVolume: %w", err)
	}
	volumeCol := volumeColVariant.ToIDispatch()
	defer volumeCol.Release()

	var volumes []VolumeInfo
	now := time.Now().UTC()

	err = oleutil.ForEach(volumeCol, func(v *ole.VARIANT) error {
		volDisp := v.ToIDispatch()
		defer volDisp.Release()

		driveLetter := ""
		driveLetterVar, err := oleutil.GetProperty(volDisp, "DriveLetter")
		if err == nil && driveLetterVar.Value() != nil {
			driveLetter = strings.TrimSpace(driveLetterVar.ToString())
		}

		protStatus := 0
		protStatusVar, err := oleutil.GetProperty(volDisp, "ProtectionStatus")
		if err == nil && protStatusVar.Value() != nil {
			switch val := protStatusVar.Value().(type) {
			case int32:
				protStatus = int(val)
			case uint32:
				protStatus = int(val)
			case int64:
				protStatus = int(val)
			case uint64:
				protStatus = int(val)
			case int:
				protStatus = val
			}
		}

		volInfo := VolumeInfo{
			DriveLetter:      driveLetter,
			ProtectionStatus: protStatus,
		}

		// Retrieve numerical password protectors (Type 3)
		protectorIDs, err := getCOMProtectorIDs(volDisp)
		if err == nil {
			for _, pid := range protectorIDs {
				pid = strings.TrimSpace(pid)
				if pid == "" {
					continue
				}
				password, err := getCOMProtectorPassword(volDisp, pid)
				if err == nil && password != "" {
					normPass, err := NormalizeRecoveryPassword(password)
					if err == nil {
						volInfo.Protectors = append(volInfo.Protectors, KeyProtector{
							DriveLetter:      driveLetter,
							KeyID:            CleanKeyID(pid),
							RecoveryPassword: normPass,
							CreatedAt:        now,
						})
					}
				}
			}
		}

		volumes = append(volumes, volInfo)
		return nil
	})

	if err != nil {
		return nil, err
	}
	return volumes, nil
}

func getCOMProtectorIDs(volDisp *ole.IDispatch) ([]string, error) {
	methodsVar, err := oleutil.GetProperty(volDisp, "Methods_")
	if err != nil {
		return nil, err
	}
	methodsDisp := methodsVar.ToIDispatch()
	defer methodsDisp.Release()

	methodVar, err := oleutil.CallMethod(methodsDisp, "Item", "GetKeyProtectors")
	if err != nil {
		return nil, err
	}
	methodDisp := methodVar.ToIDispatch()
	defer methodDisp.Release()

	inParamsVar, err := oleutil.GetProperty(methodDisp, "InParameters")
	if err != nil {
		return nil, err
	}
	inParamsDisp := inParamsVar.ToIDispatch()
	defer inParamsDisp.Release()

	inInstVar, err := oleutil.CallMethod(inParamsDisp, "SpawnInstance_")
	if err != nil {
		return nil, err
	}
	inInstDisp := inInstVar.ToIDispatch()
	defer inInstDisp.Release()

	if _, err := oleutil.PutProperty(inInstDisp, "KeyProtectorType", 3); err != nil {
		return nil, err
	}

	outParamsVar, err := oleutil.CallMethod(volDisp, "ExecMethod_", "GetKeyProtectors", inInstDisp)
	if err != nil {
		return nil, err
	}
	outParamsDisp := outParamsVar.ToIDispatch()
	defer outParamsDisp.Release()

	retValVar, err := oleutil.GetProperty(outParamsDisp, "ReturnValue")
	if err == nil && retValVar.Val != 0 {
		return nil, fmt.Errorf("GetKeyProtectors returned code %v", retValVar.Val)
	}

	idsVar, err := oleutil.GetProperty(outParamsDisp, "VolumeKeyProtectorID")
	if err != nil || idsVar.VT == ole.VT_NULL || idsVar.VT == ole.VT_EMPTY {
		return nil, nil
	}

	sa := idsVar.ToArray()
	if sa == nil {
		return nil, nil
	}
	defer sa.Release()

	return sa.ToStringArray(), nil
}

func getCOMProtectorPassword(volDisp *ole.IDispatch, protectorID string) (string, error) {
	methodsVar, err := oleutil.GetProperty(volDisp, "Methods_")
	if err != nil {
		return "", err
	}
	methodsDisp := methodsVar.ToIDispatch()
	defer methodsDisp.Release()

	methodVar, err := oleutil.CallMethod(methodsDisp, "Item", "GetKeyProtectorNumericalPassword")
	if err != nil {
		return "", err
	}
	methodDisp := methodVar.ToIDispatch()
	defer methodDisp.Release()

	inParamsVar, err := oleutil.GetProperty(methodDisp, "InParameters")
	if err != nil {
		return "", err
	}
	inParamsDisp := inParamsVar.ToIDispatch()
	defer inParamsDisp.Release()

	inInstVar, err := oleutil.CallMethod(inParamsDisp, "SpawnInstance_")
	if err != nil {
		return "", err
	}
	inInstDisp := inInstVar.ToIDispatch()
	defer inInstDisp.Release()

	if _, err := oleutil.PutProperty(inInstDisp, "VolumeKeyProtectorID", protectorID); err != nil {
		return "", err
	}

	outParamsVar, err := oleutil.CallMethod(volDisp, "ExecMethod_", "GetKeyProtectorNumericalPassword", inInstDisp)
	if err != nil {
		return "", err
	}
	outParamsDisp := outParamsVar.ToIDispatch()
	defer outParamsDisp.Release()

	retValVar, err := oleutil.GetProperty(outParamsDisp, "ReturnValue")
	if err == nil && retValVar.Val != 0 {
		return "", fmt.Errorf("GetKeyProtectorNumericalPassword returned code %v", retValVar.Val)
	}

	passVar, err := oleutil.GetProperty(outParamsDisp, "NumericalPassword")
	if err != nil {
		return "", err
	}
	return passVar.ToString(), nil
}

// getBitLockerVolumesPowerShell is a fallback method querying CIM via PowerShell.
func getBitLockerVolumesPowerShell() ([]VolumeInfo, error) {
	psCmd := `$ErrorActionPreference = 'Stop'
$vols = @(Get-CimInstance -Namespace root\cimv2\Security\MicrosoftVolumeEncryption -ClassName Win32_EncryptableVolume)
$out = @()
foreach ($v in $vols) {
    $drive = $v.DriveLetter
    $prot = $v.ProtectionStatus
    $protectors = @()
    try {
        $pRes = Invoke-CimMethod -InputObject $v -MethodName GetKeyProtectors -Arguments @{KeyProtectorType=3}
        if ($pRes.VolumeKeyProtectorID) {
            foreach ($pid in $pRes.VolumeKeyProtectorID) {
                $pwdRes = Invoke-CimMethod -InputObject $v -MethodName GetKeyProtectorNumericalPassword -Arguments @{VolumeKeyProtectorID=$pid}
                if ($pwdRes.NumericalPassword) {
                    $protectors += @{
                        drive_letter = $drive
                        key_id = $pid
                        recovery_password = $pwdRes.NumericalPassword
                    }
                }
            }
        }
    } catch {}
    $out += @{
        drive_letter = $drive
        protection_status = $prot
        protectors = $protectors
    }
}
$out | ConvertTo-Json -Compress -Depth 4`

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psCmd)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("powershell CIM query failed: %w", err)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	now := time.Now().UTC()

	type psProtector struct {
		DriveLetter      string `json:"drive_letter"`
		KeyID            string `json:"key_id"`
		RecoveryPassword string `json:"recovery_password"`
	}
	type psVolume struct {
		DriveLetter      string        `json:"drive_letter"`
		ProtectionStatus int           `json:"protection_status"`
		Protectors       []psProtector `json:"protectors"`
	}

	var psVolumes []psVolume
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &psVolumes); err != nil {
			return nil, err
		}
	} else if strings.HasPrefix(trimmed, "{") {
		var single psVolume
		if err := json.Unmarshal([]byte(trimmed), &single); err != nil {
			return nil, err
		}
		psVolumes = append(psVolumes, single)
	}

	var results []VolumeInfo
	for _, pv := range psVolumes {
		vInfo := VolumeInfo{
			DriveLetter:      pv.DriveLetter,
			ProtectionStatus: pv.ProtectionStatus,
		}
		for _, prot := range pv.Protectors {
			normPass, err := NormalizeRecoveryPassword(prot.RecoveryPassword)
			if err == nil {
				vInfo.Protectors = append(vInfo.Protectors, KeyProtector{
					DriveLetter:      pv.DriveLetter,
					KeyID:            CleanKeyID(prot.KeyID),
					RecoveryPassword: normPass,
					CreatedAt:        now,
				})
			}
		}
		results = append(results, vInfo)
	}

	return results, nil
}
