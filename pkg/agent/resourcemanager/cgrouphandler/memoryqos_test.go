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

package cgrouphandler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

// TestSetMemoryQoS_Integration Test Memory QoS handler with different pod instance
func TestSetMemoryQoS_Integration(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	cgroupVersion, err := cgroup.DetectCgroupVersion("/sys/fs/cgroup")
	if err != nil {
		t.Fatalf("Failed to detect cgroup version: %v", err)
	}

	t.Logf("Detected cgroup version: %s", cgroupVersion)

	cgroupMgr := cgroup.NewCgroupManager("", TestCgroupRootPath, "")
	handler := NewCgroupHandler(cgroupMgr, cgroupVersion)

	// pods with different QoS level in kubernetes
	testCases := []struct {
		name     string
		qosLevel int64
		podSpec  corev1.PodSpec
	}{
		{
			name:     "High Priority QoS Level 2 - Guaranteed Pod",
			qosLevel: 2,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Normal Priority QoS Level 1 - Burstable Pod",
			qosLevel: 1,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Low Priority QoS Level 0 - Burstable Pod",
			qosLevel: 0,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Idle QoS Level -1 - BestEffort Pod",
			qosLevel: -1,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ns := "default"
			podName := fmt.Sprintf("memory-qos-test-pod-%d", tc.qosLevel)
			image := "docker.io/library/alpine:latest"

			pod, clientset, err := createTestPodWithSpec(ns, podName, image, tc.podSpec)
			if err != nil {
				t.Fatalf("Failed to create test pod: %v", err)
			}

			t.Cleanup(func() {
				_ = clientset.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
			})

			actualQoSClass := pod.Status.QOSClass
			t.Logf("Pod %s has QoS Class: %s", pod.Name, actualQoSClass)

			err = handler.SetMemoryQoS(pod.UID, actualQoSClass, tc.qosLevel)
			if err != nil {
				t.Fatalf("Failed to set Memory QoS level: %v", err)
			}

			err = verifyMemoryQoSSettings(t, handler, pod.UID, actualQoSClass, tc.qosLevel, cgroupVersion)
			if err != nil {
				t.Fatalf("Failed to verify Memory QoS settings: %v", err)
			}

			t.Logf("Successfully set and verified Memory QoS level %d for pod %s", tc.qosLevel, pod.Name)
		})
	}
}

// verifyMemoryQoSSettings Verify the Memory QoS Setting
func verifyMemoryQoSSettings(t *testing.T, handler *CgroupHandler, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64, cgroupVersion string) error {
	cgroupMgr := handler.cgroupMgr
	cgroupPath, err := cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupMemorySubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	t.Logf("Pod cgroup path: %s", cgroupPath)

	switch cgroupVersion {
	case "v1":
		return verifyMemoryCgroupV1Settings(t, cgroupPath, qosLevel)
	case "v2":
		return verifyMemoryCgroupV2Settings(t, cgroupPath, qosLevel)
	default:
		return fmt.Errorf("unsupported cgroup version: %s", cgroupVersion)
	}
}

// verifyMemoryCgroupV1Settings Verify the Memory QoS Setting cgroup v1 version
func verifyMemoryCgroupV1Settings(t *testing.T, cgroupPath string, qosLevel int64) error {
	qosLevelFile := filepath.Join(cgroupPath, cgroup.MemoryQoSLevelFile)

	if _, err := os.Stat(qosLevelFile); os.IsNotExist(err) {
		t.Logf("Cgroup v1 Memory QoS level file does not exist: %s", qosLevelFile)
		return nil
	}

	content, err := os.ReadFile(qosLevelFile)
	if err != nil {
		return fmt.Errorf("failed to read Memory QoS level file: %v", err)
	}

	actualQoSLevel, err := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse Memory QoS level: %v", err)
	}

	// For memory QoS, normalize the level (only 0 and -1 are supported)
	expectedQoSLevel := qosLevel
	if qosLevel < 0 {
		expectedQoSLevel = -1
	} else {
		expectedQoSLevel = 0
	}

	if actualQoSLevel != expectedQoSLevel {
		return fmt.Errorf("expected Memory QoS level %d, got %d", expectedQoSLevel, actualQoSLevel)
	}

	t.Logf("Verified cgroup v1 Memory QoS level: %d", actualQoSLevel)
	return nil
}

// verifyMemoryCgroupV2Settings Verify the Memory QoS Setting cgroup v2 version
func verifyMemoryCgroupV2Settings(t *testing.T, cgroupPath string, qosLevel int64) error {
	// Verify memory.high (soft limit)
	expectedMemoryHigh := utils.CalculateMemoryHighFromQoSLevel(qosLevel)
	memoryHighFile := filepath.Join(cgroupPath, cgroup.MemoryHighFileV2)

	if _, err := os.Stat(memoryHighFile); err == nil {
		content, err := os.ReadFile(memoryHighFile)
		if err != nil {
			return fmt.Errorf("failed to read memory.high file: %v", err)
		}

		highValue := strings.TrimSpace(string(content))
		if expectedMemoryHigh == 0 {
			if highValue != "max" {
				return fmt.Errorf("expected memory.high value 'max', got %s", highValue)
			}
		} else {
			actualMemoryHigh, err := strconv.ParseUint(highValue, 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse memory.high: %v", err)
			}

			if actualMemoryHigh != expectedMemoryHigh {
				return fmt.Errorf("expected memory.high %d, got %d", expectedMemoryHigh, actualMemoryHigh)
			}
		}

		t.Logf("Verified cgroup v2 memory.high: %s", highValue)
	} else {
		t.Logf("memory.high file does not exist: %s", memoryHighFile)
	}

	// Verify memory.low (minimum guarantee)
	expectedMemoryLow := utils.CalculateMemoryLowFromQoSLevel(qosLevel)
	memoryLowFile := filepath.Join(cgroupPath, cgroup.MemoryLowFileV2)

	if _, err := os.Stat(memoryLowFile); err == nil {
		content, err := os.ReadFile(memoryLowFile)
		if err != nil {
			return fmt.Errorf("failed to read memory.low file: %v", err)
		}

		actualMemoryLow, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse memory.low: %v", err)
		}

		if actualMemoryLow != expectedMemoryLow {
			return fmt.Errorf("expected memory.low %d, got %d", expectedMemoryLow, actualMemoryLow)
		}

		t.Logf("Verified cgroup v2 memory.low: %d", actualMemoryLow)
	} else {
		t.Logf("memory.low file does not exist: %s", memoryLowFile)
	}

	// Verify memory.min (minimum reservation)
	expectedMemoryMin := utils.CalculateMemoryMinFromQoSLevel(qosLevel)
	memoryMinFile := filepath.Join(cgroupPath, cgroup.MemoryMinFileV2)

	if _, err := os.Stat(memoryMinFile); err == nil {
		content, err := os.ReadFile(memoryMinFile)
		if err != nil {
			return fmt.Errorf("failed to read memory.min file: %v", err)
		}

		actualMemoryMin, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse memory.min: %v", err)
		}

		if actualMemoryMin != expectedMemoryMin {
			return fmt.Errorf("expected memory.min %d, got %d", expectedMemoryMin, actualMemoryMin)
		}

		t.Logf("Verified cgroup v2 memory.min: %d", actualMemoryMin)
	} else {
		t.Logf("memory.min file does not exist: %s", memoryMinFile)
	}

	return nil
}

// checkMemoryUsage Check memory usage
func checkMemoryUsage(t *testing.T, handler *CgroupHandler, podUID types.UID, qosClass corev1.PodQOSClass, cgroupVersion string) error {
	cgroupMgr := handler.cgroupMgr
	cgroupPath, err := cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupMemorySubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	var memoryUsageFile string
	switch cgroupVersion {
	case "v1":
		memoryUsageFile = filepath.Join(cgroupPath, cgroup.MemoryUsageFile)
	case "v2":
		memoryUsageFile = filepath.Join(cgroupPath, cgroup.MemoryUsageFileV2)
	}

	if _, err := os.Stat(memoryUsageFile); os.IsNotExist(err) {
		return fmt.Errorf("Memory usage file does not exist: %s", memoryUsageFile)
	}

	content, err := os.ReadFile(memoryUsageFile)
	if err != nil {
		return fmt.Errorf("failed to read Memory usage file: %v", err)
	}

	t.Logf("Memory usage file content: %s", string(content))
	return nil
}
