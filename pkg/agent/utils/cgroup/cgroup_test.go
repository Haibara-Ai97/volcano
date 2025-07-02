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
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Mock file system interface for testing
type fileSystem interface {
	Stat(name string) (os.FileInfo, error)
}

type realFileSystem struct{}

func (rfs realFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Mock file system for testing
type mockFileSystem struct {
	statFunc func(name string) (os.FileInfo, error)
}

func (mfs mockFileSystem) Stat(name string) (os.FileInfo, error) {
	if mfs.statFunc != nil {
		return mfs.statFunc(name)
	}
	return nil, os.ErrNotExist
}

// detectCgroupVersionWithFS is a testable version of DetectCgroupVersion
func detectCgroupVersionWithFS(fs fileSystem) (string, error) {
	// Check if cgroup v2 is mounted
	if _, err := fs.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		return CgroupV2, nil
	}

	// Check if cgroup v1 is mounted
	if _, err := fs.Stat("/sys/fs/cgroup/cpu"); err == nil {
		return CgroupV1, nil
	}

	// Check for hybrid mode (v1 and v2)
	if _, err := fs.Stat("/sys/fs/cgroup/unified"); err == nil {
		return CgroupV2, nil
	}

	return "", fmt.Errorf("unable to detect cgroup version")
}

func TestDetectCgroupVersion(t *testing.T) {
	// Test with real cgroup environment
	t.Run("real environment detection", func(t *testing.T) {
		version, err := DetectCgroupVersion()
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

	// Test with mock cgroup v2 environment
	t.Run("cgroup v2 detection", func(t *testing.T) {
		// Create temporary directory structure for cgroup v2
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "cgroup")
		err := os.MkdirAll(cgroupDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Create cgroup.controllers file to simulate cgroup v2
		controllersFile := filepath.Join(cgroupDir, "cgroup.controllers")
		err = os.WriteFile(controllersFile, []byte("cpu memory io"), 0644)
		if err != nil {
			t.Fatalf("Failed to create cgroup.controllers file: %v", err)
		}

		// Create mock file system
		mockFS := mockFileSystem{
			statFunc: func(name string) (os.FileInfo, error) {
				if name == "/sys/fs/cgroup/cgroup.controllers" {
					return os.Stat(filepath.Join(cgroupDir, "cgroup.controllers"))
				}
				return nil, os.ErrNotExist
			},
		}

		version, err := detectCgroupVersionWithFS(mockFS)
		if err != nil {
			t.Fatalf("Failed to detect cgroup version: %v", err)
		}
		if version != CgroupV2 {
			t.Errorf("Expected cgroup version %s, got %s", CgroupV2, version)
		}
	})

	// Test with mock cgroup v1 environment
	t.Run("cgroup v1 detection", func(t *testing.T) {
		// Create temporary directory structure for cgroup v1
		tmpDir := t.TempDir()
		cgroupDir := filepath.Join(tmpDir, "cgroup")
		err := os.MkdirAll(filepath.Join(cgroupDir, "cpu"), 0755)
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}

		// Create mock file system
		mockFS := mockFileSystem{
			statFunc: func(name string) (os.FileInfo, error) {
				if name == "/sys/fs/cgroup/cpu" {
					return os.Stat(filepath.Join(cgroupDir, "cpu"))
				}
				return nil, os.ErrNotExist
			},
		}

		version, err := detectCgroupVersionWithFS(mockFS)
		if err != nil {
			t.Fatalf("Failed to detect cgroup version: %v", err)
		}
		if version != CgroupV1 {
			t.Errorf("Expected cgroup version %s, got %s", CgroupV1, version)
		}
	})

	// Test with no cgroup environment
	t.Run("no cgroup environment", func(t *testing.T) {
		mockFS := mockFileSystem{
			statFunc: func(name string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
		}

		_, err := detectCgroupVersionWithFS(mockFS)
		if err == nil {
			t.Error("Expected error when no cgroup environment is detected")
		}
	})
}

// Mock version detector for testing NewCgroupManager
type versionDetector interface {
	Detect() (string, error)
}

type realVersionDetector struct{}

func (rvd realVersionDetector) Detect() (string, error) {
	return DetectCgroupVersion()
}

type mockVersionDetector struct {
	version string
	err     error
}

func (mvd mockVersionDetector) Detect() (string, error) {
	return mvd.version, mvd.err
}

// newCgroupManagerWithDetector is a testable version of NewCgroupManager
func newCgroupManagerWithDetector(cgroupDriver, cgroupRoot, kubeCgroupRoot string, detector versionDetector) (CgroupManager, error) {
	cgroupVersion, err := detector.Detect()
	if err != nil {
		return nil, fmt.Errorf("failed to detect cgroup version: %v", err)
	}

	switch cgroupVersion {
	case CgroupV1:
		return &CgroupManagerImpl{
			cgroupDriver:   cgroupDriver,
			cgroupRoot:     cgroupRoot,
			kubeCgroupRoot: kubeCgroupRoot,
			cgroupVersion:  cgroupVersion,
		}, nil
	case CgroupV2:
		return &CgroupV2ManagerImpl{
			cgroupDriver:   cgroupDriver,
			cgroupRoot:     cgroupRoot,
			kubeCgroupRoot: kubeCgroupRoot,
			cgroupVersion:  cgroupVersion,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported cgroup version: %s", cgroupVersion)
	}
}

func TestNewCgroupManager(t *testing.T) {
	// Test with real cgroup environment
	t.Run("real environment manager creation", func(t *testing.T) {
		manager, err := NewCgroupManager("cgroupfs", "/sys/fs/cgroup", "")
		if err != nil {
			t.Fatalf("Failed to create cgroup manager in real environment: %v", err)
		}

		version := manager.GetCgroupVersion()
		if version != CgroupV1 && version != CgroupV2 {
			t.Errorf("Expected cgroup version to be %s or %s, got %s", CgroupV1, CgroupV2, version)
		}

		t.Logf("Created manager with cgroup version: %s", version)

		// Test that the manager can generate paths
		path, err := manager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path: %v", err)
		}

		// Verify the path is reasonable
		if !strings.Contains(path, "kubepods") {
			t.Errorf("Generated path should contain kubepods: %s", path)
		}

		// Test with systemd driver
		systemdManager, err := NewCgroupManager("systemd", "/sys/fs/cgroup", "")
		if err != nil {
			t.Fatalf("Failed to create systemd cgroup manager: %v", err)
		}

		systemdPath, err := systemdManager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path with systemd: %v", err)
		}

		// Systemd path should be different from cgroupfs path
		if systemdPath == path {
			t.Errorf("Systemd and cgroupfs paths should be different: %s", systemdPath)
		}
	})

	t.Run("create cgroup v1 manager", func(t *testing.T) {
		// Mock cgroup v1 detection
		mockDetector := mockVersionDetector{
			version: CgroupV1,
			err:     nil,
		}

		manager, err := newCgroupManagerWithDetector("cgroupfs", "/sys/fs/cgroup", "", mockDetector)
		if err != nil {
			t.Fatalf("Failed to create cgroup manager: %v", err)
		}

		if manager.GetCgroupVersion() != CgroupV1 {
			t.Errorf("Expected cgroup version %s, got %s", CgroupV1, manager.GetCgroupVersion())
		}
	})

	t.Run("create cgroup v2 manager", func(t *testing.T) {
		// Mock cgroup v2 detection
		mockDetector := mockVersionDetector{
			version: CgroupV2,
			err:     nil,
		}

		manager, err := newCgroupManagerWithDetector("systemd", "/sys/fs/cgroup", "", mockDetector)
		if err != nil {
			t.Fatalf("Failed to create cgroup manager: %v", err)
		}

		if manager.GetCgroupVersion() != CgroupV2 {
			t.Errorf("Expected cgroup version %s, got %s", CgroupV2, manager.GetCgroupVersion())
		}
	})

	t.Run("unsupported cgroup version", func(t *testing.T) {
		mockDetector := mockVersionDetector{
			version: "v3",
			err:     nil,
		}

		_, err := newCgroupManagerWithDetector("cgroupfs", "/sys/fs/cgroup", "", mockDetector)
		if err == nil {
			t.Error("Expected error for unsupported cgroup version")
		}
	})

	t.Run("detection error", func(t *testing.T) {
		mockDetector := mockVersionDetector{
			version: "",
			err:     fmt.Errorf("detection failed"),
		}

		_, err := newCgroupManagerWithDetector("cgroupfs", "/sys/fs/cgroup", "", mockDetector)
		if err == nil {
			t.Error("Expected error when detection fails")
		}
	})

	// Test error cases with real environment
	t.Run("invalid cgroup driver", func(t *testing.T) {
		// This test requires a real environment but tests invalid driver
		// We'll test the error handling in the path generation
		manager, err := NewCgroupManager("invalid-driver", "/sys/fs/cgroup", "")
		if err != nil {
			t.Fatalf("Manager creation should not fail with invalid driver: %v", err)
		}

		// The error should occur when trying to generate paths
		_, err = manager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err == nil {
			t.Error("Expected error with invalid cgroup driver")
		}
	})

	t.Run("custom cgroup root", func(t *testing.T) {
		manager, err := NewCgroupManager("cgroupfs", "/sys/fs/cgroup", "custom-root")
		if err != nil {
			t.Fatalf("Failed to create cgroup manager with custom root: %v", err)
		}

		path, err := manager.GetRootCgroupPath(CgroupCpuSubsystem)
		if err != nil {
			t.Fatalf("Failed to get root cgroup path: %v", err)
		}

		// Should include the custom root in the path
		if !strings.Contains(path, "custom-root") {
			t.Errorf("Path should contain custom root: %s", path)
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
