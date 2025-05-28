# CRD Installer Utility Implementation

## Requirements
- Implement a new utility in the `cmd` directory that can install either hub CRDs or member CRDs based on a flag
- Add a sidecar container to both the hub-agent and member-agent Helm charts that uses this utility
- Ensure the utility can be built and included in the appropriate Docker images

## Additional comments from user
- The utility should be configurable via command line flags
- Integration with existing Helm charts is required

## Plan
### Phase 1: Create CRD Installer Utility
1. Create a new directory for the CRD installer in `cmd/crdinstaller`
2. Implement the main function with flag parsing
3. Implement the logic to install hub or member CRDs
4. Add tests for the utility

### Phase 2: Update Docker Files
1. Update the existing Dockerfiles to include the new CRD installer utility
2. Create a new Dockerfile specifically for the CRD installer if needed

### Phase 3: Update Helm Charts
1. Update the hub-agent Helm chart to include the CRD installer sidecar
2. Update the member-agent Helm chart to include the CRD installer sidecar
3. Add appropriate configuration values to the charts' values.yaml files

### Phase 4: Testing
1. Test the CRD installer utility locally
2. Test the modified Helm charts with the new sidecar

## Decisions
- We will create a new command-line utility in `cmd/crdinstaller`
- The utility will accept flags to determine which CRDs to install (hub or member)
- We'll leverage existing Go CRD installation functionality from the kubefleet codebase
- We'll add the utility as a sidecar container in both hub-agent and member-agent deployments

## Implementation Details

### CRD Installer Utility
1. Created a new Go program in `cmd/crdinstaller/main.go` that:
   - Takes command-line arguments for mode (hub/member)
   - Uses controller-runtime to access the Kubernetes API
   - Reads CRDs from a hardcoded path (`/workspace/config/crd/bases`)
   - Installs or updates CRDs based on the specified mode

2. Created a Dockerfile at `docker/crd-installer.Dockerfile` that:
   - Builds the CRD installer binary
   - Copies the CRDs to the expected path within the container
   - Uses the bitnami/kubectl image as the base for the container

3. Updated Helm charts to use the CRD installer as an init container:
   - Added `crdInstaller` configuration to both charts' values.yaml files
   - Added an init container to both deployment templates
   - Made the installer configurable via Helm values

## Changes Made

1. Added new files:
   - `cmd/crdinstaller/main.go`: The CRD installer utility
   - `docker/crd-installer.Dockerfile`: Dockerfile for building the CRD installer image

2. Modified existing files:
   - `charts/hub-agent/values.yaml`: Added crdInstaller configuration section
   - `charts/member-agent/values.yaml`: Added crdInstaller configuration section
   - `charts/hub-agent/templates/deployment.yaml`: Added CRD installer init container
   - `charts/member-agent/templates/deployment.yaml`: Added CRD installer init container

## Before/After Comparison

### Before:
- CRDs had to be installed manually or separately using kubectl apply
- No built-in mechanism for CRD installation during deployment

### After:
- Automated CRD installation as part of the deployment process
- Clear separation between hub and member cluster CRDs
- Configurable options for waiting for CRDs to be established
- Proper error handling and logging

## References
- KubeFleet repository structure
- Hub-agent and member-agent chart structures

## Tasks Checklist
- [x] Phase 1: Create CRD Installer Utility
  - [x] Task 1.1: Create the crdinstaller directory and main.go file
  - [x] Task 1.2: Implement flags and command-line parsing
  - [x] Task 1.3: Implement logic to install hub or member CRDs
  - [ ] Task 1.4: Add tests for the utility

- [x] Phase 2: Update Docker Files
  - [x] Task 2.1: Update or create Dockerfiles for the CRD installer

- [x] Phase 3: Update Helm Charts
  - [x] Task 3.1: Modify hub-agent chart to include CRD installer sidecar
  - [x] Task 3.2: Modify member-agent chart to include CRD installer sidecar
  - [x] Task 3.3: Update values.yaml files with new configuration options

- [ ] Phase 4: Testing
  - [ ] Task 4.1: Test CRD installer utility
  - [ ] Task 4.2: Test Helm chart changes
