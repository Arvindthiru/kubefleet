/*
Copyright 2025 The KubeFleet Authors.

Licensed u	// Record the latest policy snapshot associated with the placement.
	latestPolicySnapshot, _, err := r.determinePolicySnapshot(ctx, placementNamespacedName, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Collect the scheduled clusters by the corresponding placement with the latest policy snapshot.
	// Note: collectScheduledClusters still expects concrete type, will need refactoring next
	clusterPolicySnapshot, ok := latestPolicySnapshot.(*placementv1beta1.ClusterSchedulingPolicySnapshot)
	if !ok {
		return nil, nil, fmt.Errorf("expected ClusterSchedulingPolicySnapshot but got %T", latestPolicySnapshot)
	}
	scheduledBindings, toBeDeletedBindings, err := r.collectScheduledClusters(ctx, placementNamespacedName.Name, clusterPolicySnapshot, updateRun)e Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package updaterun

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/kubefleet-dev/kubefleet/apis/cluster/v1beta1"
	placementv1beta1 "github.com/kubefleet-dev/kubefleet/apis/placement/v1beta1"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/annotations"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/condition"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/controller"
	"github.com/kubefleet-dev/kubefleet/pkg/utils/overrider"
)

// initialize initializes the ClusterStagedUpdateRun object with all the stages computed during the initialization.
// This function is called only once during the initialization of the ClusterStagedUpdateRun.
func (r *Reconciler) initialize(
	ctx context.Context,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
) ([]*placementv1beta1.ClusterResourceBinding, []*placementv1beta1.ClusterResourceBinding, error) {
	// Validate the Placement object referenced by the StagedUpdateRun.
	placementNamespacedName, err := r.validatePlacement(ctx, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Record the latest policy snapshot associated with the placement.
	latestPolicySnapshot, _, err := r.determinePolicySnapshot(ctx, placementNamespacedName, updateRun)
	if err != nil {
		return nil, nil, err
	}
	// Collect the scheduled clusters by the corresponding placement with the latest policy snapshot.
	scheduledBindingObjs, toBeDeletedBindingObjs, err := r.collectScheduledClusters(ctx, placementNamespacedName, latestPolicySnapshot, updateRun)
	if err != nil {
		return nil, nil, err
	}

	// Convert interfaces back to concrete types for compatibility with other functions that haven't been refactored yet
	scheduledBindings := make([]*placementv1beta1.ClusterResourceBinding, len(scheduledBindingObjs))
	toBeDeletedBindings := make([]*placementv1beta1.ClusterResourceBinding, len(toBeDeletedBindingObjs))

	for i, bindingObj := range scheduledBindingObjs {
		if crb, ok := bindingObj.(*placementv1beta1.ClusterResourceBinding); ok {
			scheduledBindings[i] = crb
		} else {
			// This should only happen for StagedUpdateRun with ResourceBinding, but initialize currently only handles ClusterStagedUpdateRun
			return nil, nil, fmt.Errorf("expected ClusterResourceBinding but got %T - initialize function needs further refactoring for namespace-scoped resources", bindingObj)
		}
	}

	for i, bindingObj := range toBeDeletedBindingObjs {
		if crb, ok := bindingObj.(*placementv1beta1.ClusterResourceBinding); ok {
			toBeDeletedBindings[i] = crb
		} else {
			return nil, nil, fmt.Errorf("expected ClusterResourceBinding but got %T - initialize function needs further refactoring for namespace-scoped resources", bindingObj)
		}
	}

	// Compute the stages based on the StagedUpdateStrategy.
	if err := r.generateStagesByStrategy(ctx, scheduledBindings, toBeDeletedBindings, updateRun); err != nil {
		return nil, nil, err
	}
	// Record the override snapshots associated with each cluster.
	if err := r.recordOverrideSnapshots(ctx, placementNamespacedName.Name, updateRun); err != nil {
		return nil, nil, err
	}

	return scheduledBindings, toBeDeletedBindings, r.recordInitializationSucceeded(ctx, updateRun)
}

// validatePlacement validates the Placement object (CRP or RP) referenced by the StagedUpdateRun.
func (r *Reconciler) validatePlacement(ctx context.Context, updateRun placementv1beta1.StagedUpdateRunObj) (types.NamespacedName, error) {
	updateRunRef := klog.KObj(updateRun)

	// Get placement name from the updateRun spec using interface methods
	placementName := updateRun.GetStagedUpdateRunSpec().PlacementName

	// Create NamespacedName for the placement
	// For ClusterStagedUpdateRun, namespace will be empty (cluster-scoped)
	// For StagedUpdateRun, namespace will be the actual namespace (namespace-scoped)
	namespacedName := types.NamespacedName{
		Name:      placementName,
		Namespace: updateRun.GetNamespace(),
	}

	// Fetch the placement object using the utility function
	// This automatically determines whether to fetch CRP or RP based on namespace presence
	placement, err := controller.FetchPlacementFromNamespacedName(ctx, r.Client, namespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			placementNotFoundErr := controller.NewUserError(fmt.Errorf("parent placement not found"))
			klog.ErrorS(err, "Failed to get placement", "placement", placementName, "namespace", updateRun.GetNamespace(), "stagedUpdateRun", updateRunRef)
			return types.NamespacedName{}, fmt.Errorf("%w: %s", errInitializedFailed, placementNotFoundErr.Error())
		}
		klog.ErrorS(err, "Failed to get placement", "placement", placementName, "namespace", updateRun.GetNamespace(), "stagedUpdateRun", updateRunRef)
		return types.NamespacedName{}, controller.NewAPIServerError(true, err)
	}

	// Check if the Placement has an external rollout strategy using interface methods
	placementSpec := placement.GetPlacementSpec()
	if placementSpec.Strategy.Type != placementv1beta1.ExternalRolloutStrategyType {
		klog.V(2).InfoS("The placement does not have an external rollout strategy", "placement", placementName, "namespace", updateRun.GetNamespace(), "stagedUpdateRun", updateRunRef)
		wrongRolloutTypeErr := controller.NewUserError(errors.New("parent placement does not have an external rollout strategy, current strategy: " + string(placementSpec.Strategy.Type)))
		return types.NamespacedName{}, fmt.Errorf("%w: %s", errInitializedFailed, wrongRolloutTypeErr.Error())
	}

	// Update the apply strategy in the updateRun status using interface methods
	updateRunStatus := updateRun.GetStagedUpdateRunStatus()
	updateRunStatus.ApplyStrategy = placementSpec.Strategy.ApplyStrategy
	updateRun.SetStagedUpdateRunStatus(*updateRunStatus)

	return namespacedName, nil
}

// determinePolicySnapshot retrieves the latest policy snapshot associated with the Placement,
// and validates it and records it in the StagedUpdateRun status.
func (r *Reconciler) determinePolicySnapshot(
	ctx context.Context,
	placementNamespacedName types.NamespacedName,
	updateRun placementv1beta1.StagedUpdateRunObj,
) (placementv1beta1.PolicySnapshotObj, int, error) {
	updateRunRef := klog.KObj(updateRun)

	// Get the latest policy snapshot using the utility function
	// This automatically determines whether to fetch ClusterSchedulingPolicySnapshot or SchedulingPolicySnapshot
	policySnapshotList, err := controller.FetchLatestPolicySnapshot(ctx, r.Client, placementNamespacedName)
	if err != nil {
		klog.ErrorS(err, "Failed to list the latest policy snapshots", "placement", placementNamespacedName, "stagedUpdateRun", updateRunRef)
		// err can be retried.
		return nil, -1, controller.NewAPIServerError(true, err)
	}

	policySnapshotObjs := policySnapshotList.GetPolicySnapshotObjs()
	if len(policySnapshotObjs) != 1 {
		if len(policySnapshotObjs) > 1 {
			err := controller.NewUnexpectedBehaviorError(fmt.Errorf("more than one (%d in actual) latest policy snapshots associated with the placement: %v", len(policySnapshotObjs), placementNamespacedName))
			klog.ErrorS(err, "Failed to find the latest policy snapshot", "placement", placementNamespacedName, "stagedUpdateRun", updateRunRef)
			// no more retries for this error.
			return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
		}
		// no latest policy snapshot found.
		err := fmt.Errorf("no latest policy snapshot associated with the placement: %v", placementNamespacedName)
		klog.ErrorS(err, "Failed to find the latest policy snapshot", "placement", placementNamespacedName, "stagedUpdateRun", updateRunRef)
		// again, no more retries here.
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	latestPolicySnapshot := policySnapshotObjs[0]
	policyIndex, foundIndex := latestPolicySnapshot.GetLabels()[placementv1beta1.PolicyIndexLabel]
	if !foundIndex || len(policyIndex) == 0 {
		noIndexErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("policy snapshot `%s` does not have a policy index label", latestPolicySnapshot.GetName()))
		klog.ErrorS(noIndexErr, "Failed to get the policy index from the latestPolicySnapshot", "placement", placementNamespacedName, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
		// no more retries here.
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, noIndexErr.Error())
	}

	// Update the policy snapshot index in updateRun status using interface methods
	updateRunStatus := updateRun.GetStagedUpdateRunStatus()
	updateRunStatus.PolicySnapshotIndexUsed = policyIndex

	// For pickAll policy, the observed cluster count is not included in the policy snapshot.
	// We set it to -1. It will be validated in the binding stages.
	// If policy is nil, it's default to pickAll.
	clusterCount := -1
	policySnapshotSpec := latestPolicySnapshot.GetPolicySnapshotSpec()
	if policySnapshotSpec.Policy != nil {
		if policySnapshotSpec.Policy.PlacementType == placementv1beta1.PickNPlacementType {
			// Extract cluster count using interface - no switch needed
			count, err := annotations.ExtractNumOfClustersFromPolicySnapshot(latestPolicySnapshot)
			if err != nil {
				annErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("%w: the policy snapshot `%s` doesn't have valid cluster count annotation", err, latestPolicySnapshot.GetName()))
				klog.ErrorS(annErr, "Failed to get the cluster count from the latestPolicySnapshot", "placement", placementNamespacedName, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
				// no more retries here.
				return nil, -1, fmt.Errorf("%w, %s", errInitializedFailed, annErr.Error())
			}
			clusterCount = count
		} else if policySnapshotSpec.Policy.PlacementType == placementv1beta1.PickFixedPlacementType {
			clusterCount = len(policySnapshotSpec.Policy.ClusterNames)
		}
	}
	updateRunStatus.PolicyObservedClusterCount = clusterCount
	updateRun.SetStagedUpdateRunStatus(*updateRunStatus)

	klog.V(2).InfoS("Found the latest policy snapshot", "latestPolicySnapshot", latestPolicySnapshot.GetName(), "observedClusterCount", updateRunStatus.PolicyObservedClusterCount, "stagedUpdateRun", updateRunRef)

	if !condition.IsConditionStatusTrue(latestPolicySnapshot.GetCondition(string(placementv1beta1.PolicySnapshotScheduled)), latestPolicySnapshot.GetGeneration()) {
		scheduleErr := fmt.Errorf("policy snapshot `%s` not fully scheduled yet", latestPolicySnapshot.GetName())
		klog.ErrorS(scheduleErr, "The policy snapshot is not scheduled successfully", "placement", placementNamespacedName, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
		return nil, -1, fmt.Errorf("%w: %s", errInitializedFailed, scheduleErr.Error())
	}
	return latestPolicySnapshot, clusterCount, nil
}

// collectScheduledClusters retrieves the schedules clusters from the latest policy snapshot
// and lists all the bindings according to its SchedulePolicyTrackingLabel.
func (r *Reconciler) collectScheduledClusters(
	ctx context.Context,
	placementKey types.NamespacedName,
	latestPolicySnapshot placementv1beta1.PolicySnapshotObj,
	updateRun placementv1beta1.StagedUpdateRunObj,
) ([]placementv1beta1.BindingObj, []placementv1beta1.BindingObj, error) {
	updateRunRef := klog.KObj(updateRun)

	// List all the bindings using utility function - automatically determines CRB vs RB based on namespace
	bindingObjs, err := controller.ListBindingsFromKey(ctx, r.Client, placementKey)
	if err != nil {
		klog.ErrorS(err, "Failed to list bindings", "placement", placementKey, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
		// list err can be retried.
		return nil, nil, controller.NewAPIServerError(true, err)
	}
	var toBeDeletedBindings, selectedBindings []placementv1beta1.BindingObj
	for i, binding := range bindingObjs {
		bindingSpec := binding.GetBindingSpec()
		if bindingSpec.SchedulingPolicySnapshotName == latestPolicySnapshot.GetName() {
			if bindingSpec.State == placementv1beta1.BindingStateUnscheduled {
				klog.V(2).InfoS("Found an unscheduled binding with the latest policy snapshot, delete it", "binding", binding.GetName(), "placement", placementKey,
					"latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
				toBeDeletedBindings = append(toBeDeletedBindings, bindingObjs[i])
			} else {
				klog.V(2).InfoS("Found a scheduled binding", "binding", binding.GetName(), "placement", placementKey,
					"latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
				selectedBindings = append(selectedBindings, bindingObjs[i])
			}
		} else {
			if bindingSpec.State != placementv1beta1.BindingStateUnscheduled {
				stateErr := fmt.Errorf("binding `%s` with old policy snapshot %s has state %s, we might observe a transient state, need retry", binding.GetName(), bindingSpec.SchedulingPolicySnapshotName, bindingSpec.State)
				klog.V(2).InfoS("Found a not-unscheduled binding with old policy snapshot, retrying", "binding", binding.GetName(), "placement", placementKey,
					"latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
				// Transient state can be retried.
				return nil, nil, stateErr
			}
			klog.V(2).InfoS("Found an unscheduled binding with old policy snapshot", "binding", binding.GetName(), "cluster", bindingSpec.TargetCluster,
				"placement", placementKey, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
			toBeDeletedBindings = append(toBeDeletedBindings, bindingObjs[i])
		}
	}

	if len(selectedBindings) == 0 && len(toBeDeletedBindings) == 0 {
		nobindingErr := fmt.Errorf("no scheduled or to-be-deleted bindings found for the latest policy snapshot %s", latestPolicySnapshot.GetName())
		klog.ErrorS(nobindingErr, "Failed to collect bindings", "placement", placementKey, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
		// no more retries here.
		return nil, nil, fmt.Errorf("%w: %s", errInitializedFailed, nobindingErr.Error())
	}

	// Use interface methods for status access
	updateRunStatus := updateRun.GetStagedUpdateRunStatus()
	if updateRunStatus.PolicyObservedClusterCount == -1 {
		// For pickAll policy, the observed cluster count is not included in the policy snapshot. We set it to the number of selected bindings.
		// TODO (wantjian): refactor this part to update PolicyObservedClusterCount in one place.
		updateRunStatus.PolicyObservedClusterCount = len(selectedBindings)
		updateRun.SetStagedUpdateRunStatus(*updateRunStatus)
	} else if updateRunStatus.PolicyObservedClusterCount != len(selectedBindings) {
		countErr := controller.NewUnexpectedBehaviorError(fmt.Errorf("the number of selected bindings %d is not equal to the observed cluster count %d", len(selectedBindings), updateRunStatus.PolicyObservedClusterCount))
		klog.ErrorS(countErr, "Failed to collect bindings", "placement", placementKey, "latestPolicySnapshot", latestPolicySnapshot.GetName(), "stagedUpdateRun", updateRunRef)
		// no more retries here.
		return nil, nil, fmt.Errorf("%w: %s", errInitializedFailed, countErr.Error())
	}
	return selectedBindings, toBeDeletedBindings, nil
}

// generateStagesByStrategy computes the stages based on the ClusterStagedUpdateStrategy referenced by the ClusterStagedUpdateRun.
func (r *Reconciler) generateStagesByStrategy(
	ctx context.Context,
	scheduledBindings []*placementv1beta1.ClusterResourceBinding,
	toBeDeletedBindings []*placementv1beta1.ClusterResourceBinding,
	updateRun *placementv1beta1.ClusterStagedUpdateRun,
) error {
	updateRunRef := klog.KObj(updateRun)
	// Fetch the StagedUpdateStrategy referenced by StagedUpdateStrategyName.
	var updateStrategy placementv1beta1.ClusterStagedUpdateStrategy
	if err := r.Client.Get(ctx, client.ObjectKey{Name: updateRun.Spec.StagedUpdateStrategyName}, &updateStrategy); err != nil {
		klog.ErrorS(err, "Failed to get StagedUpdateStrategy", "stagedUpdateStrategy", updateRun.Spec.StagedUpdateStrategyName, "clusterStagedUpdateRun", updateRunRef)
		if apierrors.IsNotFound(err) {
			// we won't continue or retry the initialization if the StagedUpdateStrategy is not found.
			strategyNotFoundErr := controller.NewUserError(errors.New("referenced clusterStagedUpdateStrategy not found: " + updateRun.Spec.StagedUpdateStrategyName))
			return fmt.Errorf("%w: %s", errInitializedFailed, strategyNotFoundErr.Error())
		}
		// other err can be retried.
		return controller.NewAPIServerError(true, err)
	}

	// This won't change even if the stagedUpdateStrategy changes or is deleted after the updateRun is initialized.
	updateRun.Status.StagedUpdateStrategySnapshot = &updateStrategy.Spec
	// Remove waitTime from the updateRun status for AfterStageTask for type Approval.
	removeWaitTimeFromUpdateRunStatus(updateRun)

	// Compute the update stages.
	// Convert concrete binding arrays to interface arrays
	scheduledBindingObjs := controller.ConvertCRBArrayToBindingObjs(scheduledBindings)
	if err := r.computeRunStageStatus(ctx, scheduledBindingObjs, updateRun); err != nil {
		return err
	}
	toBeDeletedClusters := make([]placementv1beta1.ClusterUpdatingStatus, len(toBeDeletedBindings))
	for i, binding := range toBeDeletedBindings {
		klog.V(2).InfoS("Adding a cluster to the delete stage", "cluster", binding.Spec.TargetCluster, "clusterStagedUpdateStrategy", updateStrategy.Name, "clusterStagedUpdateRun", updateRunRef)
		toBeDeletedClusters[i].ClusterName = binding.Spec.TargetCluster
	}
	// Sort the clusters in the deletion stage by their names.
	sort.Slice(toBeDeletedClusters, func(i, j int) bool {
		return toBeDeletedClusters[i].ClusterName < toBeDeletedClusters[j].ClusterName
	})
	updateRun.Status.DeletionStageStatus = &placementv1beta1.StageUpdatingStatus{
		StageName: placementv1beta1.UpdateRunDeleteStageName,
		Clusters:  toBeDeletedClusters,
	}
	return nil
}

// computeRunStageStatus computes the stages based on the StagedUpdateStrategy and records them in the StagedUpdateRun status.
func (r *Reconciler) computeRunStageStatus(
	ctx context.Context,
	scheduledBindings []placementv1beta1.BindingObj,
	updateRun placementv1beta1.StagedUpdateRunObj,
) error {
	updateRunRef := klog.KObj(updateRun)

	// Use interface methods to access updateRun data
	updateRunSpec := updateRun.GetStagedUpdateRunSpec()
	updateRunStatus := updateRun.GetStagedUpdateRunStatus()
	updateStrategyName := updateRunSpec.StagedUpdateStrategyName

	// Map to track clusters and ensure they appear in one and only one stage.
	allSelectedClusters := make(map[string]struct{}, len(scheduledBindings))
	allPlacedClusters := make(map[string]struct{})
	for _, binding := range scheduledBindings {
		bindingSpec := binding.GetBindingSpec()
		allSelectedClusters[bindingSpec.TargetCluster] = struct{}{}
	}
	stagesStatus := make([]placementv1beta1.StageUpdatingStatus, 0, len(updateRunStatus.StagedUpdateStrategySnapshot.Stages))

	// Apply the label selectors from the StagedUpdateStrategy to filter the clusters.
	for _, stage := range updateRunStatus.StagedUpdateStrategySnapshot.Stages {
		if err := validateAfterStageTask(stage.AfterStageTasks); err != nil {
			klog.ErrorS(err, "Failed to validate the after stage tasks", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "clusterStagedUpdateRun", updateRunRef)
			// no more retries here.
			invalidAfterStageErr := controller.NewUserError(fmt.Errorf("the after stage tasks are invalid, clusterStagedUpdateStrategy: %s, stage: %s, err: %s", updateStrategyName, stage.Name, err.Error()))
			return fmt.Errorf("%w: %s", errInitializedFailed, invalidAfterStageErr.Error())
		}

		curStageUpdatingStatus := placementv1beta1.StageUpdatingStatus{StageName: stage.Name}
		var curStageClusters []clusterv1beta1.MemberCluster
		labelSelector, err := metav1.LabelSelectorAsSelector(stage.LabelSelector)
		if err != nil {
			klog.ErrorS(err, "Failed to convert label selector", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "labelSelector", stage.LabelSelector, "clusterStagedUpdateRun", updateRunRef)
			// no more retries here.
			invalidLabelErr := controller.NewUserError(fmt.Errorf("the stage label selector is invalid, clusterStagedUpdateStrategy: %s, stage: %s, err: %s", updateStrategyName, stage.Name, err.Error()))
			return fmt.Errorf("%w: %s", errInitializedFailed, invalidLabelErr.Error())
		}
		// List all the clusters that match the label selector.
		var clusterList clusterv1beta1.MemberClusterList
		if err := r.Client.List(ctx, &clusterList, &client.ListOptions{LabelSelector: labelSelector}); err != nil {
			klog.ErrorS(err, "Failed to list clusters for the stage", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "labelSelector", stage.LabelSelector, "clusterStagedUpdateRun", updateRunRef)
			// list err can be retried.
			return controller.NewAPIServerError(true, err)
		}

		// Intersect the selected clusters with the clusters in the stage.
		for _, cluster := range clusterList.Items {
			if _, ok := allSelectedClusters[cluster.Name]; ok {
				if _, ok := allPlacedClusters[cluster.Name]; ok {
					// a cluster can only appear in one stage.
					dupErr := controller.NewUserError(fmt.Errorf("cluster `%s` appears in more than one stages", cluster.Name))
					klog.ErrorS(dupErr, "Failed to compute the stage", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "clusterStagedUpdateRun", updateRunRef)
					// no more retries here.
					return fmt.Errorf("%w: %s", errInitializedFailed, dupErr.Error())
				}
				if stage.SortingLabelKey != nil {
					// interpret the label values as integers.
					if _, err := strconv.Atoi(cluster.Labels[*stage.SortingLabelKey]); err != nil {
						keyErr := controller.NewUserError(fmt.Errorf("the sorting label `%s:%s` on cluster `%s` is not valid: %s", *stage.SortingLabelKey, cluster.Labels[*stage.SortingLabelKey], cluster.Name, err.Error()))
						klog.ErrorS(keyErr, "Failed to sort clusters in the stage", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "clusterStagedUpdateRun", updateRunRef)
						// no more retries here.
						return fmt.Errorf("%w: %s", errInitializedFailed, keyErr.Error())
					}
				}
				curStageClusters = append(curStageClusters, cluster)
				allPlacedClusters[cluster.Name] = struct{}{}
			}
		}

		// Check if the stage is empty.
		if len(curStageClusters) == 0 {
			// since we allow no selected bindings, a stage can be empty.
			klog.InfoS("No cluster is selected for the stage", "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "clusterStagedUpdateRun", updateRunRef)
		} else {
			// Sort the clusters in the stage based on the SortingLabelKey and cluster name.
			sort.Slice(curStageClusters, func(i, j int) bool {
				if stage.SortingLabelKey == nil {
					return curStageClusters[i].Name < curStageClusters[j].Name
				}
				labelI, _ := strconv.Atoi(curStageClusters[i].Labels[*stage.SortingLabelKey])
				labelJ, _ := strconv.Atoi(curStageClusters[j].Labels[*stage.SortingLabelKey])
				if labelI != labelJ {
					return labelI < labelJ
				}
				return curStageClusters[i].Name < curStageClusters[j].Name
			})
		}

		// Record the clusters in the stage.
		curStageUpdatingStatus.Clusters = make([]placementv1beta1.ClusterUpdatingStatus, len(curStageClusters))
		for i, cluster := range curStageClusters {
			klog.V(2).InfoS("Adding a cluster to the stage", "cluster", cluster.Name, "clusterStagedUpdateStrategy", updateStrategyName, "stage name", stage.Name, "clusterStagedUpdateRun", updateRunRef)
			curStageUpdatingStatus.Clusters[i].ClusterName = cluster.Name
		}

		// Create the after stage tasks.
		curStageUpdatingStatus.AfterStageTaskStatus = make([]placementv1beta1.AfterStageTaskStatus, len(stage.AfterStageTasks))
		for i, task := range stage.AfterStageTasks {
			curStageUpdatingStatus.AfterStageTaskStatus[i].Type = task.Type
			if task.Type == placementv1beta1.AfterStageTaskTypeApproval {
				curStageUpdatingStatus.AfterStageTaskStatus[i].ApprovalRequestName = fmt.Sprintf(placementv1beta1.ApprovalTaskNameFmt, updateRun.GetName(), stage.Name)
			}
		}
		stagesStatus = append(stagesStatus, curStageUpdatingStatus)
	}

	// Update the stages status using interface methods
	updateRunStatus.StagesStatus = stagesStatus
	updateRun.SetStagedUpdateRunStatus(*updateRunStatus)

	// Check if the clusters are all placed.
	if len(allPlacedClusters) != len(allSelectedClusters) {
		missingErr := controller.NewUserError(fmt.Errorf("some clusters are not placed in any stage"))
		missingClusters := make([]string, 0, len(allSelectedClusters)-len(allPlacedClusters))
		for cluster := range allSelectedClusters {
			if _, ok := allPlacedClusters[cluster]; !ok {
				missingClusters = append(missingClusters, cluster)
			}
		}
		// Sort the missing clusters by their names to generate a stable error message.
		sort.Strings(missingClusters)
		klog.ErrorS(missingErr, "Clusters are missing in any stage", "clusters", strings.Join(missingClusters, ", "), "clusterStagedUpdateStrategy", updateStrategyName, "clusterStagedUpdateRun", updateRunRef)
		// no more retries here, only show the first 10 missing clusters in the CRP status.
		return fmt.Errorf("%w: %s, total %d, showing up to 10: %s", errInitializedFailed, missingErr.Error(), len(missingClusters), strings.Join(missingClusters[:min(10, len(missingClusters))], ", "))
	}
	return nil
}

// validateAfterStageTask valides the afterStageTasks in the stage defined in the clusterStagedUpdateStrategy.
// The error returned from this function is not retryable.
func validateAfterStageTask(tasks []placementv1beta1.AfterStageTask) error {
	if len(tasks) == 2 && tasks[0].Type == tasks[1].Type {
		return fmt.Errorf("afterStageTasks cannot have two tasks of the same type: %s", tasks[0].Type)
	}
	for i, task := range tasks {
		if task.Type == placementv1beta1.AfterStageTaskTypeTimedWait {
			if task.WaitTime == nil {
				return fmt.Errorf("task %d of type TimedWait has wait duration set to nil", i)
			}
			if task.WaitTime.Duration <= 0 {
				return fmt.Errorf("task %d of type TimedWait has wait duration <= 0", i)
			}
		}
	}
	return nil
}

// recordOverrideSnapshots finds all the override snapshots that are associated with each cluster and record them in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordOverrideSnapshots(ctx context.Context, placementName string, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	updateRunRef := klog.KObj(updateRun)

	snapshotIndex, err := strconv.Atoi(updateRun.Spec.ResourceSnapshotIndex)
	if err != nil || snapshotIndex < 0 {
		err := controller.NewUserError(fmt.Errorf("invalid resource snapshot index `%s` provided, expected an integer >= 0", updateRun.Spec.ResourceSnapshotIndex))
		klog.ErrorS(err, "Failed to parse the resource snapshot index", "clusterStagedUpdateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	// TODO: use the lib to fetch the master resource snapshot using interface instead of concrete type
	var masterResourceSnapshot *placementv1beta1.ClusterResourceSnapshot
	labelMatcher := client.MatchingLabels{
		placementv1beta1.PlacementTrackingLabel: placementName,
		placementv1beta1.ResourceIndexLabel:     updateRun.Spec.ResourceSnapshotIndex,
	}
	resourceSnapshotList := &placementv1beta1.ClusterResourceSnapshotList{}
	if err := r.Client.List(ctx, resourceSnapshotList, labelMatcher); err != nil {
		klog.ErrorS(err, "Failed to list the clusterResourceSnapshots associated with the clusterResourcePlacement",
			"clusterResourcePlacement", placementName, "resourceSnapshotIndex", snapshotIndex, "clusterStagedUpdateRun", updateRunRef)
		// err can be retried.
		return controller.NewAPIServerError(true, err)
	}

	if len(resourceSnapshotList.Items) == 0 {
		err := controller.NewUserError(fmt.Errorf("no clusterResourceSnapshots with index `%d` found for clusterResourcePlacement `%s`", snapshotIndex, placementName))
		klog.ErrorS(err, "No specified clusterResourceSnapshots found", "clusterStagedUpdateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}

	// Look for the master clusterResourceSnapshot.
	for i, resourceSnapshot := range resourceSnapshotList.Items {
		// only master has this annotation
		if len(resourceSnapshot.Annotations[placementv1beta1.ResourceGroupHashAnnotation]) != 0 {
			masterResourceSnapshot = &resourceSnapshotList.Items[i]
			break
		}
	}

	// No clusterResourceSnapshot found
	if masterResourceSnapshot == nil {
		err := controller.NewUnexpectedBehaviorError(fmt.Errorf("no master clusterResourceSnapshot found for clusterResourcePlacement `%s` with index `%d`", placementName, snapshotIndex))
		klog.ErrorS(err, "Failed to find master clusterResourceSnapshot", "clusterStagedUpdateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	klog.V(2).InfoS("Found master clusterResourceSnapshot", "clusterResourcePlacement", placementName, "index", snapshotIndex, "clusterStagedUpdateRun", updateRunRef)

	// Fetch all the matching overrides.
	matchedCRO, matchedRO, err := overrider.FetchAllMatchingOverridesForResourceSnapshot(ctx, r.Client, r.InformerManager, updateRun.Spec.PlacementName, masterResourceSnapshot)
	if err != nil {
		klog.ErrorS(err, "Failed to find all matching overrides for the clusterStagedUpdateRun", "masterResourceSnapshot", klog.KObj(masterResourceSnapshot), "clusterStagedUpdateRun", updateRunRef)
		// no more retries here.
		return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
	}
	// Pick the overrides associated with each target cluster.
	for _, stageStatus := range updateRun.Status.StagesStatus {
		for i := range stageStatus.Clusters {
			clusterStatus := &stageStatus.Clusters[i]
			clusterStatus.ClusterResourceOverrideSnapshots, clusterStatus.ResourceOverrideSnapshots, err =
				overrider.PickFromResourceMatchedOverridesForTargetCluster(ctx, r.Client, clusterStatus.ClusterName, matchedCRO, matchedRO)
			if err != nil {
				klog.ErrorS(err, "Failed to pick the override snapshots for cluster", "cluster", clusterStatus.ClusterName, "masterResourceSnapshot", klog.KObj(masterResourceSnapshot), "clusterStagedUpdateRun", updateRunRef)
				// no more retries here.
				return fmt.Errorf("%w: %s", errInitializedFailed, err.Error())
			}
		}
	}
	return nil
}

// recordInitializationSucceeded records the successful initialization condition in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordInitializationSucceeded(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunInitializeSucceededReason,
		Message:            "ClusterStagedUpdateRun initialized successfully",
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as initialized", "clusterStagedUpdateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}

// recordInitializationFailed records the failed initialization condition in the ClusterStagedUpdateRun status.
func (r *Reconciler) recordInitializationFailed(ctx context.Context, updateRun *placementv1beta1.ClusterStagedUpdateRun, message string) error {
	meta.SetStatusCondition(&updateRun.Status.Conditions, metav1.Condition{
		Type:               string(placementv1beta1.StagedUpdateRunConditionInitialized),
		Status:             metav1.ConditionFalse,
		ObservedGeneration: updateRun.Generation,
		Reason:             condition.UpdateRunInitializeFailedReason,
		Message:            message,
	})
	if updateErr := r.Client.Status().Update(ctx, updateRun); updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update the ClusterStagedUpdateRun status as failed to initialize", "clusterStagedUpdateRun", klog.KObj(updateRun))
		// updateErr can be retried.
		return controller.NewUpdateIgnoreConflictError(updateErr)
	}
	return nil
}
