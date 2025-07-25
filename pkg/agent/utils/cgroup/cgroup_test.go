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
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const TestCgroupRootPath = "/sys/fs/cgroup"

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

// CreateTestPod create tmp pod for test cgroup path
func CreateTestPod(ns, podName, image string) (*corev1.Pod, *kubernetes.Clientset, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   image,
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

// TestCgroupV2GetPodPath_Integration Test get pod path in cgroup v2 file system
func TestCgroupV2GetPodPath_Integration(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	ns := "default"
	podName := "cgroupv2-pod-path-test"
	image := "docker.io/library/alpine:latest"

	pod, clientset, err := CreateTestPod(ns, podName, image)
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}
	qos := pod.Status.QOSClass
	podUID := pod.UID

	mgr := NewCgroupManager("", TestCgroupRootPath, "")
	cgroupPath, err := mgr.GetPodCgroupPath(qos, CgroupCpuSubsystem, podUID)
	if err != nil {
		t.Fatalf("Failed to get pod cgroup path: %v", err)
	}
	if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
		t.Errorf("Cgroup path not exist: %s", cgroupPath)
	} else {
		t.Logf("Cgroup path exist: %s", cgroupPath)
	}

	t.Cleanup(func() {
		_ = clientset.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
	})
}

// TestCgroupV2GetRootPath_Integration Test get root path in cgroup v2 file system
func TestCgroupV2GetRootPath_Integration(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	mgr := NewCgroupManager("", TestCgroupRootPath, "")
	cgroupPath, err := mgr.GetRootCgroupPath(CgroupCpuSubsystem)
	if err != nil {
		t.Fatalf("Failed to get pod cgroup path: %v", err)
	}
	if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
		t.Errorf("Cgroup path not exist: %s", cgroupPath)
	} else {
		t.Logf("Cgroup path exist: %s", cgroupPath)
	}
}

// TestCgroupV2GetQoSPath_Integration Test get pod path in cgroup v2 file system
func TestCgroupV2GetQoSPath_Integration(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	mgr := NewCgroupManager("", TestCgroupRootPath, "")
	cgroupPath, err := mgr.GetQoSCgroupPath(corev1.PodQOSBurstable, CgroupCpuSubsystem)
	if err != nil {
		t.Fatalf("Failed to get pod cgroup path: %v", err)
	}
	if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
		t.Errorf("Cgroup path not exist: %s", cgroupPath)
	} else {
		t.Logf("Cgroup path exist: %s", cgroupPath)
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
