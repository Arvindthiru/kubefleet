/*
Copyright 2025 The KubeFleet Authors.

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

// Package main contains the CRD installer utility for KubeFleet
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	// Common flags
	mode           = flag.String("mode", "", "Mode to run in: 'hub' or 'member' (required)")
	waitForSuccess = flag.Bool("wait", true, "Wait for CRDs to be established before returning")
	timeout        = flag.Int("timeout", 60, "Timeout in seconds for waiting for CRDs to be established")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// Validate required flags
	if *mode != "hub" && *mode != "member" {
		klog.Fatal("--mode flag must be either 'hub' or 'member'")
	}

	klog.Infof("Starting CRD installer in %s mode", *mode)

	// Print all flags for debugging
	flag.VisitAll(func(f *flag.Flag) {
		klog.V(2).InfoS("flag:", "name", f.Name, "value", f.Value)
	})

	// Set up controller-runtime logger
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	// Create context for API operations
	ctx := ctrl.SetupSignalHandler()

	// Get Kubernetes config using controller-runtime
	config := ctrl.GetConfigOrDie()

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Install CRDs from the fixed location
	const crdPath = "/workspace/config/crd/bases"
	if err := installCRDs(ctx, dynamicClient, crdPath, *mode, *waitForSuccess, *timeout); err != nil {
		klog.Fatalf("Failed to install CRDs: %v", err)
	}

	klog.Infof("Successfully installed %s CRDs", *mode)
}

// Removed getKubernetesConfig - using controller-runtime's approach instead

// installCRDs installs the CRDs from the specified directory based on the mode.
func installCRDs(ctx context.Context, client dynamic.Interface, crdPath, mode string, wait bool, timeoutSeconds int) error {
	// CRD GVR
	crdGVR := schema.GroupVersionResource{
		Group:    "apiextensions.k8s.io",
		Version:  "v1",
		Resource: "customresourcedefinitions",
	}

	// Set of CRD filenames to install based on mode
	crdFilesToInstall := sets.New[string]()

	// Walk through the CRD directory and collect filenames
	if err := filepath.WalkDir(crdPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Only process yaml files
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		// Process based on mode
		filename := filepath.Base(path)
		isHubCRD := isHubCRD(filename)
		isMemberCRD := isMemberCRD(filename)

		switch mode {
		case "hub":
			if isHubCRD {
				crdFilesToInstall.Insert(path)
			}
		case "member":
			if isMemberCRD {
				crdFilesToInstall.Insert(path)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("failed to walk CRD directory: %w", err)
	}

	if crdFilesToInstall.Len() == 0 {
		return fmt.Errorf("no CRDs found for mode %s in directory %s", mode, crdPath)
	}

	klog.Infof("Found %d CRDs to install for mode %s", crdFilesToInstall.Len(), mode)

	// Install each CRD
	for path := range crdFilesToInstall {
		klog.V(2).Infof("Installing CRD from: %s", path)

		// Read and parse CRD file
		crdBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read CRD file %s: %w", path, err)
		}

		var crd unstructured.Unstructured
		if err := yaml.Unmarshal(crdBytes, &crd); err != nil {
			return fmt.Errorf("failed to unmarshal CRD from %s: %w", path, err)
		}

		// Apply CRD
		crdName := crd.GetName()
		klog.V(2).Infof("Creating/updating CRD: %s", crdName)

		_, err = client.Resource(crdGVR).Create(ctx, &crd, metav1.CreateOptions{})
		if err != nil {
			// If already exists, try to update
			if err.Error() == "already exists" || err.Error() == "customresourcedefinitions.apiextensions.k8s.io already exists" {
				klog.V(2).Infof("CRD %s already exists, updating", crdName)
				_, err = client.Resource(crdGVR).Update(ctx, &crd, metav1.UpdateOptions{})
				if err != nil {
					return fmt.Errorf("failed to update CRD %s: %w", crdName, err)
				}
			} else {
				return fmt.Errorf("failed to create CRD %s: %w", crdName, err)
			}
		}

		klog.Infof("Successfully installed CRD: %s", crdName)
	}

	if wait {
		// TODO: Implement wait logic for CRDs to be established
		klog.Info("Waiting for CRDs to be established is not implemented yet")
	}

	return nil
}

// isHubCRD determines if a CRD should be installed on the hub cluster.
func isHubCRD(filename string) bool {
	// Hub contains all primary resource types that define resources
	hubCRDs := sets.New[string](
		// Cluster CRDs
		"cluster.kubernetes-fleet.io_memberclusters.yaml",
		"cluster.kubernetes-fleet.io_internalmemberclusters.yaml",
		// Placement CRDs
		"placement.kubernetes-fleet.io_clusterapprovalrequests.yaml",
		"placement.kubernetes-fleet.io_clusterresourcebindings.yaml",
		"placement.kubernetes-fleet.io_clusterresourceenvelopes.yaml",
		"placement.kubernetes-fleet.io_clusterresourceplacements.yaml",
		"placement.kubernetes-fleet.io_clusterresourceoverrides.yaml",
		"placement.kubernetes-fleet.io_clusterresourceoverridesnapshots.yaml",
		"placement.kubernetes-fleet.io_clusterresourceplacementdisruptionbudgets.yaml",
		"placement.kubernetes-fleet.io_clusterresourceplacementevictions.yaml",
		"placement.kubernetes-fleet.io_clusterresourcesnapshots.yaml",
		"placement.kubernetes-fleet.io_clusterschedulingpolicysnapshots.yaml",
		"placement.kubernetes-fleet.io_clusterstagedupdateruns.yaml",
		"placement.kubernetes-fleet.io_clusterstagedupdatestrategies.yaml",
		"placement.kubernetes-fleet.io_resourceenvelopes.yaml",
		"placement.kubernetes-fleet.io_resourceoverrides.yaml",
		"placement.kubernetes-fleet.io_resourceoverridesnapshots.yaml",
		"placement.kubernetes-fleet.io_works.yaml",
		// multicluster CRDs
		"multicluster.x-k8s.io_clusterprofiles.yaml",
	)

	return hubCRDs.Has(filename)
}

// isMemberCRD determines if a CRD should be installed on the member cluster.
func isMemberCRD(filename string) bool {
	// Member cluster contains applied resources
	memberCRDs := sets.New[string](
		"placement.kubernetes-fleet.io_appliedworks.yaml",
	)

	return memberCRDs.Has(filename)
}
