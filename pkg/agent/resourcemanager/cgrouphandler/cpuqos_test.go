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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const TestCgroupRootPath = "/sys/fs/cgroup"

// TestSetCPUQoSLevel_Integration Test CPU QoS handler with different pod instance
func TestSetCPUQoSLevel_Integration(t *testing.T) {
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
			podName := fmt.Sprintf("cpu-qos-test-pod-%d", tc.qosLevel)
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

			err = handler.SetCPUQoSLevel(pod.UID, actualQoSClass, tc.qosLevel)
			if err != nil {
				t.Fatalf("Failed to set CPU QoS level: %v", err)
			}

			err = verifyCPUQoSSettings(t, handler, pod.UID, actualQoSClass, tc.qosLevel, cgroupVersion)
			if err != nil {
				t.Fatalf("Failed to verify CPU QoS settings: %v", err)
			}

			t.Logf("Successfully set and verified CPU QoS level %d for pod %s", tc.qosLevel, pod.Name)
		})
	}
}

// createTestPodWithSpec Create pod with specific resource limit
func createTestPodWithSpec(ns, podName, image string, podSpec corev1.PodSpec) (*corev1.Pod, *kubernetes.Clientset, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
		},
		Spec: podSpec,
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, clientset, err
	}

	_, err = clientset.CoreV1().Pods(ns).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		return nil, clientset, err
	}

	var createPod *corev1.Pod
	for i := 0; i < 30; i++ {
		createPod, err = clientset.CoreV1().Pods(ns).Get(context.Background(), podName, metav1.GetOptions{})
		if err == nil && createPod != nil {
			if createPod.Status.Phase == corev1.PodRunning {
				return createPod, clientset, nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return nil, clientset, fmt.Errorf("Failed to create pod %s in namespace %s", podName, ns)
}

// verifyCPUQoSSettings Verify the CPU QoS Setting
func verifyCPUQoSSettings(t *testing.T, handler *CgroupHandler, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64, cgroupVersion string) error {
	cgroupMgr := handler.cgroupMgr
	cgroupPath, err := cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	t.Logf("Pod cgroup path: %s", cgroupPath)

	switch cgroupVersion {
	case "v1":
		return verifyCgroupV1Settings(t, cgroupPath, qosLevel)
	case "v2":
		return verifyCgroupV2Settings(t, cgroupPath, qosLevel)
	default:
		return fmt.Errorf("unsupported cgroup version: %s", cgroupVersion)
	}
}

// verifyCgroupV1Settings Verify the CPU QoS Setting cgroup v1 version
func verifyCgroupV1Settings(t *testing.T, cgroupPath string, qosLevel int64) error {
	qosLevelFile := filepath.Join(cgroupPath, cgroup.CPUQoSLevelFile)

	if _, err := os.Stat(qosLevelFile); os.IsNotExist(err) {
		t.Logf("Cgroup v1 QoS level file does not exist: %s", qosLevelFile)
		return nil
	}

	content, err := os.ReadFile(qosLevelFile)
	if err != nil {
		return fmt.Errorf("failed to read QoS level file: %v", err)
	}

	actualQoSLevel, err := strconv.ParseInt(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse QoS level: %v", err)
	}

	if actualQoSLevel != qosLevel {
		return fmt.Errorf("expected QoS level %d, got %d", qosLevel, actualQoSLevel)
	}

	t.Logf("Verified cgroup v1 QoS level: %d", actualQoSLevel)
	return nil
}

// verifyCgroupV2Settings Verify the CPU QoS Setting cgroup v2 version
func verifyCgroupV2Settings(t *testing.T, cgroupPath string, qosLevel int64) error {
	if qosLevel == -1 {
		cpuIdleFile := filepath.Join(cgroupPath, cgroup.CPUIdleFileV2)
		if _, err := os.Stat(cpuIdleFile); err == nil {
			content, err := os.ReadFile(cpuIdleFile)
			if err != nil {
				return fmt.Errorf("failed to read cpu.idle file: %v", err)
			}

			idleValue := strings.TrimSpace(string(content))
			if idleValue != "1" {
				return fmt.Errorf("expected cpu.idle value 1, got %s", idleValue)
			}

			t.Logf("Verified cgroup v2 cpu.idle: %s", idleValue)
		} else {
			t.Logf("cpu.idle file does not exist: %s", cpuIdleFile)
		}
	} else {
		expectedWeight := utils.CalculateCPUWeightFromQoSLevel(qosLevel)
		cpuWeightFile := filepath.Join(cgroupPath, cgroup.CPUWeightFileV2)

		if _, err := os.Stat(cpuWeightFile); err == nil {
			content, err := os.ReadFile(cpuWeightFile)
			if err != nil {
				return fmt.Errorf("failed to read cpu.weight file: %v", err)
			}

			actualWeight, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
			if err != nil {
				return fmt.Errorf("failed to parse cpu.weight: %v", err)
			}

			if actualWeight != expectedWeight {
				return fmt.Errorf("expected cpu.weight %d, got %d", expectedWeight, actualWeight)
			}

			t.Logf("Verified cgroup v2 cpu.weight: %d", actualWeight)
		} else {
			t.Logf("cpu.weight file does not exist: %s", cpuWeightFile)
		}

		expectedQuota := utils.CalculateCPUQuotaFromQoSLevel(qosLevel)
		if expectedQuota > 0 {
			cpuMaxFile := filepath.Join(cgroupPath, cgroup.CPUQuotaTotalFileV2)

			if _, err := os.Stat(cpuMaxFile); err == nil {
				content, err := os.ReadFile(cpuMaxFile)
				if err != nil {
					return fmt.Errorf("failed to read cpu.max file: %v", err)
				}

				actualQuota, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse cpu.max: %v", err)
				}

				if actualQuota != expectedQuota {
					return fmt.Errorf("expected cpu.max %d, got %d", expectedQuota, actualQuota)
				}

				t.Logf("Verified cgroup v2 cpu.max: %d", actualQuota)
			} else {
				t.Logf("cpu.max file does not exist: %s", cpuMaxFile)
			}
		}
	}

	return nil
}

// checkCPUUsage Check cpu usage
func checkCPUUsage(t *testing.T, handler *CgroupHandler, podUID types.UID, qosClass corev1.PodQOSClass, cgroupVersion string) error {
	cgroupMgr := handler.cgroupMgr
	cgroupPath, err := cgroupMgr.GetPodCgroupPath(qosClass, cgroup.CgroupCpuSubsystem, podUID)
	if err != nil {
		return fmt.Errorf("failed to get pod cgroup path: %v", err)
	}

	var cpuUsageFile string
	switch cgroupVersion {
	case "v1":
		cpuUsageFile = filepath.Join(cgroupPath, cgroup.CPUUsageFile)
	case "v2":
		cpuUsageFile = filepath.Join(cgroupPath, cgroup.CPUUsageFileV2)
	}

	if _, err := os.Stat(cpuUsageFile); os.IsNotExist(err) {
		return fmt.Errorf("CPU usage file does not exist: %s", cpuUsageFile)
	}

	content, err := os.ReadFile(cpuUsageFile)
	if err != nil {
		return fmt.Errorf("failed to read CPU usage file: %v", err)
	}

	t.Logf("CPU usage file content: %s", string(content))
	return nil
}
