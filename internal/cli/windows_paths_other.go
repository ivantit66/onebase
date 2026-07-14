//go:build !windows

package cli

func isMappedNetworkDrive(string) (bool, error) { return false, nil }
