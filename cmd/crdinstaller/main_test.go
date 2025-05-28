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

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestIsHubCRD(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "hub CRD",
			filename: "placement.kubernetes-fleet.io_clusterresourceplacements.yaml",
			expected: true,
		},
		{
			name:     "member CRD",
			filename: "placement.kubernetes-fleet.io_appliedworks.yaml",
			expected: false,
		},
		{
			name:     "non-existent CRD",
			filename: "some-other-crd.yaml",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHubCRD(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsMemberCRD(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "hub CRD",
			filename: "placement.kubernetes-fleet.io_clusterresourceplacements.yaml",
			expected: false,
		},
		{
			name:     "member CRD",
			filename: "placement.kubernetes-fleet.io_appliedworks.yaml",
			expected: true,
		},
		{
			name:     "non-existent CRD",
			filename: "some-other-crd.yaml",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isMemberCRD(tt.filename)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInstallCRDs(t *testing.T) {
	// Create a temporary directory for test CRDs
	tempDir, err := os.MkdirTemp("", "crd-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test CRD files
	hubCRDFile := filepath.Join(tempDir, "placement.kubernetes-fleet.io_clusterresourceplacements.yaml")
	if err := os.WriteFile(hubCRDFile, []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusterresourceplacements.placement.kubernetes-fleet.io
spec:
  group: placement.kubernetes-fleet.io
  names:
    kind: ClusterResourcePlacement
    plural: clusterresourceplacements
  scope: Cluster
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`), 0644); err != nil {
		t.Fatalf("Failed to write hub CRD file: %v", err)
	}

	memberCRDFile := filepath.Join(tempDir, "placement.kubernetes-fleet.io_appliedworks.yaml")
	if err := os.WriteFile(memberCRDFile, []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: appliedworks.placement.kubernetes-fleet.io
spec:
  group: placement.kubernetes-fleet.io
  names:
    kind: AppliedWork
    plural: appliedworks
  scope: Cluster
  versions:
    - name: v1beta1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`), 0644); err != nil {
		t.Fatalf("Failed to write member CRD file: %v", err)
	}

	// Create fake client
	fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme)

	// Test hub mode
	ctx := context.Background()
	err = installCRDs(ctx, fakeDynamicClient, tempDir, "hub", false, 60)
	assert.NoError(t, err)

	// Test member mode
	err = installCRDs(ctx, fakeDynamicClient, tempDir, "member", false, 60)
	assert.NoError(t, err)

	// Test invalid mode
	err = installCRDs(ctx, fakeDynamicClient, tempDir, "invalid", false, 60)
	assert.Error(t, err)

	// Verify CRDs were created with dynamic client - this would require more advanced setup
	// For now let's just check that the function returned without error
}
