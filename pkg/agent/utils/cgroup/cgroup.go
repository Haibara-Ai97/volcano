/*
Copyright 2024 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cgroup

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	cgroupsystemd "github.com/opencontainers/runc/libcontainer/cgroups/systemd"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type CgroupSubsystem string

const (
	CgroupMemorySubsystem CgroupSubsystem = "memory"
	CgroupCpuSubsystem    CgroupSubsystem = "cpu"
	CgroupNetCLSSubsystem CgroupSubsystem = "net_cls"

	CgroupKubeRoot string = "kubepods"

	SystemdSuffix       string = ".slice"
	PodCgroupNamePrefix string = "pod"

	// Cgroupv1 specific files
	CPUQoSLevelFile string = "cpu.qos_level"
	CPUUsageFile    string = "cpuacct.usage"

	CPUQuotaBurstFile string = "cpu.cfs_burst_us"
	CPUQuotaTotalFile string = "cpu.cfs_quota_us"

	MemoryUsageFile    string = "memory.stat"
	MemoryQoSLevelFile string = "memory.qos_level"
	MemoryLimitFile    string = "memory.limit_in_bytes"

	NetCLSFileName string = "net_cls.classid"

	CPUShareFileName string = "cpu.shares"

	// Cgroupv2 specific files
	CPUWeightFileV2 string = "cpu.weight"
	CPUUsageFileV2  string = "cpu.stat"

	CPUQuotaBurstFileV2 string = "cpu.max.burst"
	CPUQuotaTotalFileV2 string = "cpu.max"

	MemoryUsageFileV2    string = "memory.stat"
	MemoryQoSLevelFileV2 string = "memory.qos_level"
	MemoryLimitFileV2    string = "memory.max"

	NetCLSFileNameV2 string = "net_cls.classid"

	CPUShareFileNameV2 string = "cpu.weight"

	// Cgroup version constants
	CgroupV1 string = "v1"
	CgroupV2 string = "v2"

	// Default cgroup mount points
	DefaultCgroupV1MountPoint string = "/sys/fs/cgroup"
	DefaultCgroupV2MountPoint string = "/sys/fs/cgroup"

	// Cgroup driver types
	CgroupDriverSystemd  string = "systemd"
	CgroupDriverCgroupfs string = "cgroupfs"
)

type CgroupManager interface {
	GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error)
	GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error)
	GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error)
	GetCgroupVersion() string
}

type CgroupManagerImpl struct {
	// cgroupDriver is the driver that the kubelet uses to manipulate cgroups on the host (cgroupfs or systemd)
	cgroupDriver string

	// cgroupRoot is the root cgroup to use for pods.
	cgroupRoot string

	// kubeCgroupRoot sames with kubelet configuration "cgroup-root"
	kubeCgroupRoot string

	// cgroupVersion indicates the cgroup version (v1 or v2)
	cgroupVersion string
}

type CgroupV2ManagerImpl struct {
	// cgroupDriver is the driver that the kubelet uses to manipulate cgroups on the host (cgroupfs or systemd)
	cgroupDriver string

	// cgroupRoot is the root cgroup to use for pods.
	cgroupRoot string

	// kubeCgroupRoot sames with kubelet configuration "cgroup-root"
	kubeCgroupRoot string

	// cgroupVersion indicates the cgroup version (v1 or v2)
	cgroupVersion string
}

// GetCgroupDriver gets the cgroup driver from multiple sources in order of priority
func GetCgroupDriver() string {
	// 1. Try to get from environment variable
	if driver := os.Getenv("CGROUP_DRIVER"); driver != "" {
		if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
			return driver
		}
	}

	// 2. Try to read from kubelet config file
	if driver := readKubeletCgroupDriver(); driver != "" {
		return driver
	}

	// 3. Try to detect from system
	if driver, err := DetectCgroupDriver(); err == nil {
		return driver
	}

	// 4. Default fallback
	return CgroupDriverCgroupfs
}

// readKubeletCgroupDriver reads cgroup driver from kubelet config file
func readKubeletCgroupDriver() string {
	// Common kubelet config file paths
	configPaths := []string{
		"/var/lib/kubelet/config.yaml",
		"/etc/kubernetes/kubelet.conf",
		"/var/lib/kubelet/kubeadm-flags.env",
	}

	for _, configPath := range configPaths {
		if driver := parseKubeletConfig(configPath); driver != "" {
			return driver
		}
	}

	return ""
}

// parseKubeletConfig parses kubelet config file to extract cgroup driver
func parseKubeletConfig(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	content := string(data)

	// Look for cgroupDriver in YAML format
	if strings.Contains(content, "cgroupDriver:") {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "cgroupDriver:") {
				driver := strings.TrimSpace(strings.TrimPrefix(line, "cgroupDriver:"))
				if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
					return driver
				}
			}
		}
	}

	// Look for --cgroup-driver in command line arguments
	if strings.Contains(content, "--cgroup-driver") {
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "--cgroup-driver") {
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "--cgroup-driver" && i+1 < len(parts) {
						driver := parts[i+1]
						if driver == CgroupDriverSystemd || driver == CgroupDriverCgroupfs {
							return driver
						}
					}
				}
			}
		}
	}

	return ""
}

// DetectCgroupDriver detects the cgroup driver (cgroupfs or systemd) on the system
func DetectCgroupDriver() (string, error) {
	// Check if systemd is managing cgroups by looking for systemd cgroup hierarchy
	// In systemd-managed systems, there's typically a systemd slice at the root
	if _, err := os.Stat("/sys/fs/cgroup/system.slice"); err == nil {
		return CgroupDriverSystemd, nil
	}

	// Check if we can find systemd cgroup paths
	if _, err := os.Stat("/sys/fs/cgroup/systemd"); err == nil {
		return CgroupDriverSystemd, nil
	}

	// Check for cgroupfs by looking for traditional cgroup hierarchy
	// In cgroupfs systems, we typically see individual controller directories
	if _, err := os.Stat("/sys/fs/cgroup/cpu"); err == nil {
		return CgroupDriverCgroupfs, nil
	}

	// Additional check for cgroup v2 with cgroupfs driver
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		// Check if systemd is not managing this hierarchy
		if _, err := os.Stat("/sys/fs/cgroup/system.slice"); err != nil {
			return CgroupDriverCgroupfs, nil
		}
	}

	// Check for hybrid mode where systemd might be managing some controllers
	if _, err := os.Stat("/sys/fs/cgroup/unified"); err == nil {
		// In hybrid mode, check if systemd is managing the unified hierarchy
		if _, err := os.Stat("/sys/fs/cgroup/unified/system.slice"); err == nil {
			return CgroupDriverSystemd, nil
		}
		return CgroupDriverCgroupfs, nil
	}

	return "", fmt.Errorf("unable to detect cgroup driver")
}

// DetectCgroupVersion detects the cgroup version on the system
func DetectCgroupVersion() (string, error) {
	// Check if cgroup v2 is mounted
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		return CgroupV2, nil
	}

	// Check if cgroup v1 is mounted
	if _, err := os.Stat("/sys/fs/cgroup/cpu"); err == nil {
		return CgroupV1, nil
	}

	// Check for hybrid mode (v1 and v2)
	if _, err := os.Stat("/sys/fs/cgroup/unified"); err == nil {
		return CgroupV2, nil
	}

	return "", fmt.Errorf("unable to detect cgroup version")
}

// NewCgroupManager creates a new cgroup manager based on the detected cgroup version
func NewCgroupManager(cgroupDriver, cgroupRoot, kubeCgroupRoot string) CgroupManager {
	cgroupVersion, err := DetectCgroupVersion()
	if err != nil {
		return nil
	}

	// Auto-detect cgroupDriver if not provided
	if cgroupDriver == "" {
		cgroupDriver = GetCgroupDriver()
	}

	switch version := cgroupVersion; version {
	case CgroupV1:
		return &CgroupManagerImpl{
			cgroupDriver:   cgroupDriver,
			cgroupRoot:     cgroupRoot,
			kubeCgroupRoot: kubeCgroupRoot,
			cgroupVersion:  cgroupVersion,
		}
	case CgroupV2:
		return &CgroupV2ManagerImpl{
			cgroupDriver:   cgroupDriver,
			cgroupRoot:     cgroupRoot,
			kubeCgroupRoot: kubeCgroupRoot,
			cgroupVersion:  cgroupVersion,
		}
	default:
		return nil
	}
}

// GetCgroupVersion returns the cgroup version
func (c *CgroupManagerImpl) GetCgroupVersion() string {
	return c.cgroupVersion
}

// GetCgroupVersion returns the cgroup version
func (c *CgroupV2ManagerImpl) GetCgroupVersion() string {
	return c.cgroupVersion
}

func (c *CgroupManagerImpl) GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupV2ManagerImpl) GetRootCgroupPath(cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	// In cgroup v2, all controllers are unified under a single hierarchy
	return filepath.Join(c.cgroupRoot, cgroupPath), err
}

func (c *CgroupManagerImpl) GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupV2ManagerImpl) GetQoSCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	// In cgroup v2, all controllers are unified under a single hierarchy
	return filepath.Join(c.cgroupRoot, cgroupPath), err
}

func (c *CgroupManagerImpl) GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}
	cgroupName = append(cgroupName, getPodCgroupNameSuffix(podUID))

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	return filepath.Join(c.cgroupRoot, string(cgroupSubsystem), cgroupPath), err
}

func (c *CgroupV2ManagerImpl) GetPodCgroupPath(qos corev1.PodQOSClass, cgroupSubsystem CgroupSubsystem, podUID types.UID) (string, error) {
	cgroupName := []string{CgroupKubeRoot}
	if c.kubeCgroupRoot != "" {
		cgroupName = append([]string{c.kubeCgroupRoot}, cgroupName...)
	}
	switch qos {
	case corev1.PodQOSBurstable:
		cgroupName = append(cgroupName, "burstable")
	case corev1.PodQOSBestEffort:
		cgroupName = append(cgroupName, "besteffort")
	}
	cgroupName = append(cgroupName, getPodCgroupNameSuffix(podUID))

	cgroupPath, err := c.CgroupNameToCgroupPath(cgroupName)
	if err != nil {
		return "", err
	}
	// In cgroup v2, all controllers are unified under a single hierarchy
	return filepath.Join(c.cgroupRoot, cgroupPath), err
}

func (c *CgroupManagerImpl) CgroupNameToCgroupPath(cgroupName []string) (string, error) {
	switch c.cgroupDriver {
	case "cgroupfs":
		return CgroupName(cgroupName).ToCgroupfs()
	case "systemd":
		return CgroupName(cgroupName).ToSystemd()
	default:
		return "", fmt.Errorf("unsupported cgroup driver: %s", c.cgroupDriver)
	}
}

func (c *CgroupV2ManagerImpl) CgroupNameToCgroupPath(cgroupName []string) (string, error) {
	switch c.cgroupDriver {
	case "cgroupfs":
		return CgroupName(cgroupName).ToCgroupfs()
	case "systemd":
		return CgroupName(cgroupName).ToSystemd()
	default:
		return "", fmt.Errorf("unsupported cgroup driver: %s", c.cgroupDriver)
	}
}

type CgroupName []string

func (cgroupName CgroupName) ToSystemd() (string, error) {
	if len(cgroupName) == 0 || (len(cgroupName) == 1 && cgroupName[0] == "") {
		return "/", nil
	}

	newparts := []string{}
	for _, part := range cgroupName {
		part = escapeSystemdCgroupName(part)
		newparts = append(newparts, part)
	}

	result, err := cgroupsystemd.ExpandSlice(strings.Join(newparts, "-") + SystemdSuffix)
	if err != nil {
		return "", fmt.Errorf("error converting cgroup name [%v] to systemd format: %v", cgroupName, err)
	}
	return result, nil
}

func (cgroupName CgroupName) ToCgroupfs() (string, error) {
	return "/" + path.Join(cgroupName...), nil
}

func escapeSystemdCgroupName(part string) string {
	return strings.Replace(part, "-", "_", -1)
}

// getPodCgroupNameSuffix returns the last element of the pod CgroupName identifier
func getPodCgroupNameSuffix(podUID types.UID) string {
	return PodCgroupNamePrefix + string(podUID)
}
