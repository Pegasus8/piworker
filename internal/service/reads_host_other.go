//go:build !linux

package service

// probeHost reports nothing where host figures are not cheaply available.
func probeHost(string) HostStatus { return HostStatus{} }
