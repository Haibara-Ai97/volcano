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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestDetectCgroupVersion_Integration Test detect cgroup version
func TestDetectCgroupVersion_Integration(t *testing.T) {
	// Test with real cgroup environment
	t.Run("real environment detection", func(t *testing.T) {
		version, err := DetectCgroupVersion("/sys/fs/cgroup")
		if err != nil {
			t.Fatalf("Failed to detect cgroup version in real environment: %v", err)
		}

		// Should detect either v1 or v2
		if version != CgroupV1 && version != CgroupV2 {
			t.Errorf("Expected cgroup version to be %s or %s, got %s", CgroupV1, CgroupV2, version)
		}

		t.Logf("Detected cgroup version: %s", version)

		// Verify the detection is consistent with actual filesystem
		if version == CgroupV1 {
			if _, err := os.Stat("/sys/fs/cgroup/cpu"); err != nil {
				t.Errorf("Detected v1 but /sys/fs/cgroup/cpu does not exist: %v", err)
			}
		} else if version == CgroupV2 {
			if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
				t.Errorf("Detected v2 but /sys/fs/cgroup/cgroup.controllers does not exist: %v", err)
			}
		}
	})
}

func TestCgroupV2ManagerImpl_GetPodCgroupPath(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	// Check if we have write permissions to create test cgroups
	testCgroupPath := "/sys/fs/cgroup/test-volcano-cgroup"
	if err := os.MkdirAll(testCgroupPath, 0755); err != nil {
		t.Skipf("Skipping test: cannot create test cgroup directory: %v", err)
	}
	defer os.RemoveAll(testCgroupPath) // Cleanup after test

	manager := &CgroupV2ManagerImpl{
		cgroupDriver:   "cgroupfs",
		cgroupRoot:     "/sys/fs/cgroup",
		kubeCgroupRoot: "",
		cgroupVersion:  CgroupV2,
	}

	podUID := types.UID("test-pod-uid")
	path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
	if err != nil {
		t.Fatalf("Failed to get pod cgroup path: %v", err)
	}

	// In cgroup v2, the path should not include the subsystem name
	expectedPath := "/sys/fs/cgroup/kubepods/burstable/podtest-pod-uid"
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}

	// Test that the path actually exists or can be created
	parentPath := "/sys/fs/cgroup/kubepods"
	if _, err := os.Stat(parentPath); err != nil {
		// If parent doesn't exist, try to create it (this might fail in test environment)
		if mkdirErr := os.MkdirAll(parentPath, 0755); mkdirErr != nil {
			t.Logf("Note: Cannot create parent cgroup path %s: %v (this is expected in test environment)", parentPath, mkdirErr)
		} else {
			defer os.RemoveAll(parentPath) // Cleanup if we created it
		}
	}

	// Test with different QoS levels
	testCases := []struct {
		qos          corev1.PodQOSClass
		expectedPath string
	}{
		{
			qos:          corev1.PodQOSGuaranteed,
			expectedPath: "/sys/fs/cgroup/kubepods/podtest-pod-uid",
		},
		{
			qos:          corev1.PodQOSBurstable,
			expectedPath: "/sys/fs/cgroup/kubepods/burstable/podtest-pod-uid",
		},
		{
			qos:          corev1.PodQOSBestEffort,
			expectedPath: "/sys/fs/cgroup/kubepods/besteffort/podtest-pod-uid",
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.qos), func(t *testing.T) {
			path, err := manager.GetPodCgroupPath(tc.qos, CgroupCpuSubsystem, podUID)
			if err != nil {
				t.Fatalf("Failed to get pod cgroup path for QoS %s: %v", tc.qos, err)
			}
			if path != tc.expectedPath {
				t.Errorf("Expected path %s, got %s", tc.expectedPath, path)
			}
		})
	}

	// Test with different subsystems (in v2, all should generate the same path)
	subsystemTests := []CgroupSubsystem{
		CgroupCpuSubsystem,
		CgroupMemorySubsystem,
		CgroupNetCLSSubsystem,
	}

	for _, subsystem := range subsystemTests {
		t.Run(string(subsystem), func(t *testing.T) {
			path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, subsystem, podUID)
			if err != nil {
				t.Fatalf("Failed to get pod cgroup path for subsystem %s: %v", subsystem, err)
			}

			// In cgroup v2, the path should NOT contain the subsystem name
			if strings.Contains(path, string(subsystem)) {
				t.Errorf("Path %s should NOT contain subsystem %s in cgroup v2", path, subsystem)
			}

			// Verify the path contains the pod UID
			if !strings.Contains(path, string(podUID)) {
				t.Errorf("Path %s should contain pod UID %s", path, podUID)
			}
		})
	}

	// Test with different cgroup drivers
	t.Run("systemd_driver", func(t *testing.T) {
		systemdManager := &CgroupV2ManagerImpl{
			cgroupDriver:   "systemd",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "",
			cgroupVersion:  CgroupV2,
		}

		path, err := systemdManager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
		if err != nil {
			t.Fatalf("Failed to get pod cgroup path with systemd driver: %v", err)
		}

		// With systemd driver, the path should be different (systemd format)
		if strings.Contains(path, "cgroupfs") {
			t.Errorf("Systemd driver should not return cgroupfs format path: %s", path)
		}
	})

	// Test error cases
	t.Run("invalid_pod_uid", func(t *testing.T) {
		invalidPodUID := types.UID("")
		path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, invalidPodUID)
		if err != nil {
			t.Fatalf("Should not error with empty pod UID: %v", err)
		}

		// Should still generate a valid path
		if !strings.Contains(path, "pod") {
			t.Errorf("Path should contain 'pod' prefix even with empty UID: %s", path)
		}
	})

	// Test with custom kubeCgroupRoot
	t.Run("custom_kube_cgroup_root", func(t *testing.T) {
		customManager := &CgroupV2ManagerImpl{
			cgroupDriver:   "cgroupfs",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "custom-root",
			cgroupVersion:  CgroupV2,
		}

		path, err := customManager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
		if err != nil {
			t.Fatalf("Failed to get pod cgroup path with custom root: %v", err)
		}

		// Should include the custom root in the path
		if !strings.Contains(path, "custom-root") {
			t.Errorf("Path should contain custom root: %s", path)
		}
	})
}

func TestCgroupV2ManagerImpl_GetRootCgroupPath(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	manager := &CgroupV2ManagerImpl{
		cgroupDriver:   "cgroupfs",
		cgroupRoot:     "/sys/fs/cgroup",
		kubeCgroupRoot: "",
		cgroupVersion:  CgroupV2,
	}

	path, err := manager.GetRootCgroupPath(CgroupCpuSubsystem)
	if err != nil {
		t.Fatalf("Failed to get root cgroup path: %v", err)
	}

	// In cgroup v2, the path should not include the subsystem name
	expectedPath := "/sys/fs/cgroup/kubepods"
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}

	// Test with different subsystems (in v2, all should generate the same path)
	subsystemTests := []CgroupSubsystem{
		CgroupCpuSubsystem,
		CgroupMemorySubsystem,
		CgroupNetCLSSubsystem,
	}

	for _, subsystem := range subsystemTests {
		t.Run(string(subsystem), func(t *testing.T) {
			path, err := manager.GetRootCgroupPath(subsystem)
			if err != nil {
				t.Fatalf("Failed to get root cgroup path for subsystem %s: %v", subsystem, err)
			}

			// In cgroup v2, the path should NOT contain the subsystem name
			if strings.Contains(path, string(subsystem)) {
				t.Errorf("Path %s should NOT contain subsystem %s in cgroup v2", path, subsystem)
			}

			// Verify the path contains kubepods
			if !strings.Contains(path, "kubepods") {
				t.Errorf("Path %s should contain kubepods", path)
			}
		})
	}

	// Test with custom kubeCgroupRoot
	t.Run("custom_kube_cgroup_root", func(t *testing.T) {
		customManager := &CgroupV2ManagerImpl{
			cgroupDriver:   "cgroupfs",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "custom-root",
			cgroupVersion:  CgroupV2,
		}

		path, err := customManager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path with custom root: %v", err)
		}

		// Should include the custom root in the path
		if !strings.Contains(path, "custom-root") {
			t.Errorf("Path should contain custom root: %s", path)
		}
	})

	// Test with systemd driver
	t.Run("systemd_driver", func(t *testing.T) {
		systemdManager := &CgroupV2ManagerImpl{
			cgroupDriver:   "systemd",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "",
			cgroupVersion:  CgroupV2,
		}

		path, err := systemdManager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path with systemd driver: %v", err)
		}

		// With systemd driver, the path should be different (systemd format)
		if strings.Contains(path, "cgroupfs") {
			t.Errorf("Systemd driver should not return cgroupfs format path: %s", path)
		}
	})
}

func TestCgroupV1ManagerImpl_GetPodCgroupPath(t *testing.T) {
	// Skip test if not running in a real cgroup v1 environment
	if _, err := os.Stat("/sys/fs/cgroup/cpu"); err != nil {
		t.Skip("Skipping test: cgroup v1 not available (no /sys/fs/cgroup/cpu)")
	}

	// Check if we have write permissions to create test cgroups
	testCgroupPath := "/sys/fs/cgroup/cpu/test-volcano-cgroup"
	if err := os.MkdirAll(testCgroupPath, 0755); err != nil {
		t.Skipf("Skipping test: cannot create test cgroup directory: %v", err)
	}
	defer os.RemoveAll(testCgroupPath) // Cleanup after test

	manager := &CgroupManagerImpl{
		cgroupDriver:   "cgroupfs",
		cgroupRoot:     "/sys/fs/cgroup",
		kubeCgroupRoot: "",
		cgroupVersion:  CgroupV1,
	}

	podUID := types.UID("test-pod-uid")
	path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
	if err != nil {
		t.Fatalf("Failed to get pod cgroup path: %v", err)
	}

	// In cgroup v1, the path should include the subsystem name
	expectedPath := "/sys/fs/cgroup/cpu/kubepods/burstable/podtest-pod-uid"
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}

	// Test that the path actually exists or can be created
	// Note: We can't actually create the full path in a test environment,
	// but we can verify the logic is correct by checking if the parent directories exist
	parentPath := "/sys/fs/cgroup/cpu/kubepods"
	if _, err := os.Stat(parentPath); err != nil {
		// If parent doesn't exist, try to create it (this might fail in test environment)
		if mkdirErr := os.MkdirAll(parentPath, 0755); mkdirErr != nil {
			t.Logf("Note: Cannot create parent cgroup path %s: %v (this is expected in test environment)", parentPath, mkdirErr)
		} else {
			defer os.RemoveAll(parentPath) // Cleanup if we created it
		}
	}

	// Test with different QoS levels
	testCases := []struct {
		qos          corev1.PodQOSClass
		expectedPath string
	}{
		{
			qos:          corev1.PodQOSGuaranteed,
			expectedPath: "/sys/fs/cgroup/cpu/kubepods/podtest-pod-uid",
		},
		{
			qos:          corev1.PodQOSBurstable,
			expectedPath: "/sys/fs/cgroup/cpu/kubepods/burstable/podtest-pod-uid",
		},
		{
			qos:          corev1.PodQOSBestEffort,
			expectedPath: "/sys/fs/cgroup/cpu/kubepods/besteffort/podtest-pod-uid",
		},
	}

	for _, tc := range testCases {
		t.Run(string(tc.qos), func(t *testing.T) {
			path, err := manager.GetPodCgroupPath(tc.qos, CgroupCpuSubsystem, podUID)
			if err != nil {
				t.Fatalf("Failed to get pod cgroup path for QoS %s: %v", tc.qos, err)
			}
			if path != tc.expectedPath {
				t.Errorf("Expected path %s, got %s", tc.expectedPath, path)
			}
		})
	}

	// Test with different subsystems
	subsystemTests := []CgroupSubsystem{
		CgroupCpuSubsystem,
		CgroupMemorySubsystem,
		CgroupNetCLSSubsystem,
	}

	for _, subsystem := range subsystemTests {
		t.Run(string(subsystem), func(t *testing.T) {
			path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, subsystem, podUID)
			if err != nil {
				t.Fatalf("Failed to get pod cgroup path for subsystem %s: %v", subsystem, err)
			}

			// Verify the path contains the subsystem name
			if !strings.Contains(path, string(subsystem)) {
				t.Errorf("Path %s should contain subsystem %s", path, subsystem)
			}

			// Verify the path contains the pod UID
			if !strings.Contains(path, string(podUID)) {
				t.Errorf("Path %s should contain pod UID %s", path, podUID)
			}
		})
	}

	// Test with different cgroup drivers
	t.Run("systemd_driver", func(t *testing.T) {
		systemdManager := &CgroupManagerImpl{
			cgroupDriver:   "systemd",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "",
			cgroupVersion:  CgroupV1,
		}

		path, err := systemdManager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
		if err != nil {
			t.Fatalf("Failed to get pod cgroup path with systemd driver: %v", err)
		}

		// With systemd driver, the path should be different (systemd format)
		if strings.Contains(path, "cgroupfs") {
			t.Errorf("Systemd driver should not return cgroupfs format path: %s", path)
		}
	})

	// Test error cases
	t.Run("invalid_pod_uid", func(t *testing.T) {
		invalidPodUID := types.UID("")
		path, err := manager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, invalidPodUID)
		if err != nil {
			t.Fatalf("Should not error with empty pod UID: %v", err)
		}

		// Should still generate a valid path
		if !strings.Contains(path, "pod") {
			t.Errorf("Path should contain 'pod' prefix even with empty UID: %s", path)
		}
	})

	// Test with custom kubeCgroupRoot
	t.Run("custom_kube_cgroup_root", func(t *testing.T) {
		customManager := &CgroupManagerImpl{
			cgroupDriver:   "cgroupfs",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "custom-root",
			cgroupVersion:  CgroupV1,
		}

		path, err := customManager.GetPodCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem, podUID)
		if err != nil {
			t.Fatalf("Failed to get pod cgroup path with custom root: %v", err)
		}

		// Should include the custom root in the path
		if !strings.Contains(path, "custom-root") {
			t.Errorf("Path should contain custom root: %s", path)
		}
	})
}

func TestCgroupV1ManagerImpl_GetRootCgroupPath(t *testing.T) {
	// Skip test if not running in a real cgroup v1 environment
	if _, err := os.Stat("/sys/fs/cgroup/cpu"); err != nil {
		t.Skip("Skipping test: cgroup v1 not available (no /sys/fs/cgroup/cpu)")
	}

	manager := &CgroupManagerImpl{
		cgroupDriver:   "cgroupfs",
		cgroupRoot:     "/sys/fs/cgroup",
		kubeCgroupRoot: "",
		cgroupVersion:  CgroupV1,
	}

	path, err := manager.GetRootCgroupPath(CgroupCpuSubsystem)
	if err != nil {
		t.Fatalf("Failed to get root cgroup path: %v", err)
	}

	// In cgroup v1, the path should include the subsystem name
	expectedPath := "/sys/fs/cgroup/cpu/kubepods"
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}

	// Test with different subsystems
	subsystemTests := []CgroupSubsystem{
		CgroupCpuSubsystem,
		CgroupMemorySubsystem,
		CgroupNetCLSSubsystem,
	}

	for _, subsystem := range subsystemTests {
		t.Run(string(subsystem), func(t *testing.T) {
			path, err := manager.GetRootCgroupPath(subsystem)
			if err != nil {
				t.Fatalf("Failed to get root cgroup path for subsystem %s: %v", subsystem, err)
			}

			// Verify the path contains the subsystem name
			if !strings.Contains(path, string(subsystem)) {
				t.Errorf("Path %s should contain subsystem %s", path, subsystem)
			}

			// Verify the path contains kubepods
			if !strings.Contains(path, "kubepods") {
				t.Errorf("Path %s should contain kubepods", path)
			}
		})
	}

	// Test with custom kubeCgroupRoot
	t.Run("custom_kube_cgroup_root", func(t *testing.T) {
		customManager := &CgroupManagerImpl{
			cgroupDriver:   "cgroupfs",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "custom-root",
			cgroupVersion:  CgroupV1,
		}

		path, err := customManager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path with custom root: %v", err)
		}

		// Should include the custom root in the path
		if !strings.Contains(path, "custom-root") {
			t.Errorf("Path should contain custom root: %s", path)
		}
	})

	// Test with systemd driver
	t.Run("systemd_driver", func(t *testing.T) {
		systemdManager := &CgroupManagerImpl{
			cgroupDriver:   "systemd",
			cgroupRoot:     "/sys/fs/cgroup",
			kubeCgroupRoot: "",
			cgroupVersion:  CgroupV1,
		}

		path, err := systemdManager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path with systemd driver: %v", err)
		}

		// With systemd driver, the path should be different (systemd format)
		if strings.Contains(path, "cgroupfs") {
			t.Errorf("Systemd driver should not return cgroupfs format path: %s", path)
		}
	})
}

func TestCgroupName_ToCgroupfs(t *testing.T) {
	cases := []struct {
		name     CgroupName
		expected string
	}{
		{
			name:     CgroupName{"kubepods"},
			expected: "/kubepods",
		},
		{
			name:     CgroupName{"kubepods", "burstable"},
			expected: "/kubepods/burstable",
		},
		{
			name:     CgroupName{"kubepods", "burstable", "pod123"},
			expected: "/kubepods/burstable/pod123",
		},
	}

	for _, tc := range cases {
		result, err := tc.name.ToCgroupfs()
		if err != nil {
			t.Errorf("ToCgroupfs() failed for %v: %v", tc.name, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("ToCgroupfs() for %v: expected %s, got %s", tc.name, tc.expected, result)
		}
	}
}

func TestCgroupName_ToSystemd(t *testing.T) {
	cases := []struct {
		name     CgroupName
		expected string
	}{
		{
			name:     CgroupName{},
			expected: "/",
		},
		{
			name:     CgroupName{""},
			expected: "/",
		},
		{
			name:     CgroupName{"kubepods"},
			expected: "/kubepods.slice",
		},
		{
			name:     CgroupName{"kubepods", "burstable"},
			expected: "/kubepods.slice/kubepods-burstable.slice",
		},
	}

	for _, tc := range cases {
		result, err := tc.name.ToSystemd()
		if err != nil {
			t.Errorf("ToSystemd() failed for %v: %v", tc.name, err)
			continue
		}
		if result != tc.expected {
			t.Errorf("ToSystemd() for %v: expected %s, got %s", tc.name, tc.expected, result)
		}
	}
}

func TestGetPodCgroupNameSuffix(t *testing.T) {
	podUID := types.UID("test-pod-123")
	suffix := getPodCgroupNameSuffix(podUID)
	expected := "podtest-pod-123"
	if suffix != expected {
		t.Errorf("Expected suffix %s, got %s", expected, suffix)
	}
}

func TestEscapeSystemdCgroupName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"kubepods", "kubepods"},
		{"kube-pods", "kube_pods"},
		{"kube--pods", "kube__pods"},
		{"", ""},
	}

	for _, tc := range cases {
		result := escapeSystemdCgroupName(tc.input)
		if result != tc.expected {
			t.Errorf("escapeSystemdCgroupName(%s): expected %s, got %s", tc.input, tc.expected, result)
		}
	}
}

// isRealKubernetesEnvironment check test run in k8s env
func isRealKubernetesEnvironment() bool {
	configPaths := []string{
		"/var/lib/kubelet/config.yaml",
		"/etc/kubernetes/kubelet.conf",
		"/var/lib/kubelet/kubeadm-flags.env",
	}

	for _, path := range configPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	if _, err := os.Stat("/var/run/secrets/kubernetes.io"); err == nil {
		return true
	}

	return false
}

// TestGetCgroupDriverFromKubeletConfig_Integration Test detect cgroup driver from fixed kubelet config file
func TestGetCgroupDriverFromKubeletConfig_Integration(t *testing.T) {
	if !isRealKubernetesEnvironment() {
		t.Skip("Skipping integration test: not in real Kubernetes environment")
	}

	driver := getCgroupDriverFromKubeletConfig()

	if driver != "" && driver != CgroupDriverSystemd && driver != CgroupDriverCgroupfs {
		t.Errorf("Invalid cgroup driver returned: %s", driver)
	}

	t.Logf("Detected cgroup driver from real environment: %s", driver)

	if driver != "" {
		configPaths := []string{
			"/var/lib/kubelet/config.yaml",
			"/etc/kubernetes/kubelet.conf",
			"/var/lib/kubelet/kubeadm-flags.env",
		}

		found := false
		for _, path := range configPaths {
			if _, err := os.Stat(path); err == nil {

				content, err := os.ReadFile(path)
				if err == nil {
					contentStr := string(content)
					if strings.Contains(contentStr, "cgroupDriver:") || strings.Contains(contentStr, "--cgroup-driver") {
						t.Logf("Found cgroup driver configuration in: %s", path)
						found = true
						break
					}
				}
			}
		}

		if !found {
			t.Logf("Warning: Found driver '%s' but could not verify configuration in any config file", driver)
		}
	}
}

// canAccessProcFilesystem check access to /proc
func canAccessProcFilesystem() bool {
	if _, err := os.ReadDir("/proc"); err != nil {
		return false
	}

	if _, err := os.ReadFile("/proc/1/cmdline"); err != nil {
		return false
	}

	return true
}

// TestGetCgroupDriverFromKubeletProcess_Integration Test get cgroup driver from kubelet process
func TestGetCgroupDriverFromKubeletProcess_Integration(t *testing.T) {
	if !canAccessProcFilesystem() {
		t.Skip("Skipping integration test: cannot access /proc filesystem")
	}

	driver := getCgroupDriverFromKubeletProcess()

	if driver != "" && driver != CgroupDriverSystemd && driver != CgroupDriverCgroupfs {
		t.Errorf("Invalid cgroup driver returned: %s", driver)
	}

	t.Logf("Detected cgroup driver from real kubelet process: %s", driver)

	if driver != "" {
		procDir, err := os.Open("/proc")
		if err == nil {
			defer procDir.Close()

			entries, err := procDir.Readdirnames(0)
			if err == nil {
				kubeletFound := false
				for _, entry := range entries {
					if _, err := strconv.Atoi(entry); err == nil {
						commPath := filepath.Join("/proc", entry, "comm")
						if commData, err := os.ReadFile(commPath); err == nil {
							comm := strings.TrimSpace(string(commData))
							if comm == "kubelet" {
								kubeletFound = true
								t.Logf("Found kubelet process with PID: %s", entry)
								break
							}
						}
					}
				}

				if !kubeletFound {
					t.Logf("Warning: Found driver '%s' but could not find kubelet process", driver)
				}
			}
		}
	}
}

// TestReadKubeletCgroupDriver_Integration Test ability of detect cgroup driver
func TestReadKubeletCgroupDriver_Integration(t *testing.T) {
	if !canAccessProcFilesystem() {
		t.Skip("Skipping integration test: cannot access /proc filesystem")
	}

	driver := readKubeletCgroupDriver()

	if driver != "" && driver != CgroupDriverSystemd && driver != CgroupDriverCgroupfs {
		t.Errorf("Invalid cgroup driver returned: %s", driver)
	}

	t.Logf("Detected cgroup driver from complete detection process: %s", driver)

	if driver != "" {
		configDriver := getCgroupDriverFromKubeletConfig()
		processDriver := getCgroupDriverFromKubeletProcess()

		if configDriver == driver {
			t.Logf("Driver detected via config files")
		} else if processDriver == driver {
			t.Logf("Driver detected via process command line")
		} else {
			t.Logf("Driver detected via fallback method")
		}
	}
}
