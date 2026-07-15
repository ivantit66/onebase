//go:build windows

package cli

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func isMappedNetworkDrive(path string) (bool, error) {
	volume := filepath.VolumeName(strings.TrimSpace(path))
	if len(volume) != 2 || volume[1] != ':' {
		return false, nil // локальный/UNC/относительный путь
	}
	root, err := windows.UTF16PtrFromString(strings.ToUpper(volume) + `\`)
	if err != nil {
		return false, err
	}
	return windows.GetDriveType(root) == windows.DRIVE_REMOTE, nil
}
