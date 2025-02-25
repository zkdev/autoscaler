/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This file was copied and modified from the kubernetes/autoscaler project
https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-release-1.1/cluster-autoscaler/cloudprovider/aws/aws_cloud_provider.go

Modifications Copyright (c) 2017 SAP SE or an SAP affiliate company. All rights reserved.
*/

package mcm

import (
	"fmt"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/config/dynamic"
	"k8s.io/autoscaler/cluster-autoscaler/utils/errors"
	"k8s.io/klog"
	schedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	// ProviderName is the cloud provider name for MCM
	ProviderName = "mcm"

	// GPULabel is the label added to nodes with GPU resource.
	// TODO: Align on a GPU Label for Gardener.
	GPULabel = "gardener.cloud/accelerator"
)

// MCMCloudProvider implements the cloud provider interface for machine-controller-manager
// Reference: https://github.com/gardener/machine-controller-manager
type mcmCloudProvider struct {
	mcmManager         *McmManager
	machinedeployments map[types.NamespacedName]*MachineDeployment
	resourceLimiter    *cloudprovider.ResourceLimiter
}

// BuildMcmCloudProvider builds CloudProvider implementation for machine-controller-manager.
func BuildMcmCloudProvider(mcmManager *McmManager, resourceLimiter *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	if mcmManager.discoveryOpts.StaticDiscoverySpecified() {
		return buildStaticallyDiscoveringProvider(mcmManager, mcmManager.discoveryOpts.NodeGroupSpecs, resourceLimiter)
	}
	return nil, fmt.Errorf("Failed to build an mcm cloud provider: Either node group specs or node group auto discovery spec must be specified")
}

// BuildMCM builds the MCM provider and MCMmanager.
func BuildMCM(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) cloudprovider.CloudProvider {
	var mcmManager *McmManager
	var err error
	mcmManager, err = CreateMcmManager(do)

	if err != nil {
		klog.Fatalf("Failed to create MCM Manager: %v", err)
	}
	provider, err := BuildMcmCloudProvider(mcmManager, rl)
	if err != nil {
		klog.Fatalf("Failed to create MCM cloud provider: %v", err)
	}
	return provider
}

func buildStaticallyDiscoveringProvider(mcmManager *McmManager, specs []string, resourceLimiter *cloudprovider.ResourceLimiter) (*mcmCloudProvider, error) {
	mcm := &mcmCloudProvider{
		mcmManager:         mcmManager,
		machinedeployments: make(map[types.NamespacedName]*MachineDeployment),
		resourceLimiter:    resourceLimiter,
	}
	for _, spec := range specs {
		if err := mcm.addNodeGroup(spec); err != nil {
			return nil, err
		}
	}
	return mcm, nil
}

// Cleanup stops the go routine that is handling the current view of the MachineDeployment in the form of a cache
func (mcm *mcmCloudProvider) Cleanup() error {
	mcm.mcmManager.Cleanup()
	return nil
}

// addNodeGroup adds node group defined in string spec. Format:
// minNodes:maxNodes:namespace.machineDeploymentName
func (mcm *mcmCloudProvider) addNodeGroup(spec string) error {
	machinedeployment, err := buildMachineDeploymentFromSpec(spec, mcm.mcmManager)
	if err != nil {
		return err
	}
	mcm.addMachineDeployment(machinedeployment)
	return nil
}

func (mcm *mcmCloudProvider) addMachineDeployment(machinedeployment *MachineDeployment) {
	key := types.NamespacedName{Namespace: machinedeployment.Namespace, Name: machinedeployment.Name}
	mcm.machinedeployments[key] = machinedeployment
	return
}

func (mcm *mcmCloudProvider) Name() string {
	return "machine-controller-manager"
}

// NodeGroups returns all node groups configured for this cloud provider.
func (mcm *mcmCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	result := make([]cloudprovider.NodeGroup, 0, len(mcm.machinedeployments))
	for _, machinedeployment := range mcm.machinedeployments {
		if machinedeployment.maxSize == 0 {
			continue
		}
		result = append(result, machinedeployment)
	}
	return result
}

// NodeGroupForNode returns the node group for the given node.
func (mcm *mcmCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {
	if len(node.Spec.ProviderID) == 0 {
		klog.Warningf("Node %v has no providerId", node.Name)
		return nil, nil
	}

	ref, err := ReferenceFromProviderID(mcm.mcmManager, node.Spec.ProviderID)
	if err != nil {
		return nil, err
	}

	if ref == nil {
		klog.V(4).Infof("Skipped node %v, it's either been removed or it's not managed by this controller", node.Spec.ProviderID)
		return nil, nil
	}

	md, err := mcm.mcmManager.GetMachineDeploymentForMachine(ref)
	if err != nil {
		return nil, err
	}

	key := types.NamespacedName{Namespace: md.Namespace, Name: md.Name}
	_, isManaged := mcm.machinedeployments[key]
	if !isManaged {
		klog.V(4).Infof("Skipped node %v, it's not managed by this controller", node.Spec.ProviderID)
		return nil, nil
	}

	return md, nil
}

// Pricing returns pricing model for this cloud provider or error if not available.
func (mcm *mcmCloudProvider) Pricing() (cloudprovider.PricingModel, errors.AutoscalerError) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetAvailableMachineTypes get all machine types that can be requested from the cloud provider.
func (mcm *mcmCloudProvider) GetAvailableMachineTypes() ([]string, error) {
	return []string{}, nil
}

// NewNodeGroup builds a theoretical node group based on the node definition provided. The node group is not automatically
// created on the cloud provider side. The node group is not returned by NodeGroups() until it is created.
func (mcm *mcmCloudProvider) NewNodeGroup(machineType string, labels map[string]string, systemLabels map[string]string,
	taints []apiv1.Taint, extraResources map[string]resource.Quantity) (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrNotImplemented
}

// GetResourceLimiter returns struct containing limits (max, min) for resources (cores, memory etc.).
func (mcm *mcmCloudProvider) GetResourceLimiter() (*cloudprovider.ResourceLimiter, error) {
	return mcm.resourceLimiter, nil
}

// Refresh is called before every main loop and can be used to dynamically update cloud provider state.
// In particular the list of node groups returned by NodeGroups can change as a result of CloudProvider.Refresh().
func (mcm *mcmCloudProvider) Refresh() error {
	// If we don't need to check between every reconcile, we will have return nil here
	return nil
}

// GPULabel returns the label added to nodes with GPU resource.
func (mcm *mcmCloudProvider) GPULabel() string {
	return GPULabel
}

// GetAvailableGPUTypes return all available GPU types cloud provider supports
func (mcm *mcmCloudProvider) GetAvailableGPUTypes() map[string]struct{} {
	return nil
}

// Ref contains a reference to the name of the machine-deployment.
type Ref struct {
	Name      string
	Namespace string
}

// ReferenceFromProviderID extracts the Ref from providerId. It returns corresponding machine-name to providerid.
func ReferenceFromProviderID(m *McmManager, id string) (*Ref, error) {
	machines, err := m.machineLister.Machines(m.namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("Could not list machines due to error: %s", err)
	}

	var Name, Namespace string
	for _, machine := range machines {
		machineID := strings.Split(machine.Spec.ProviderID, "/")
		nodeID := strings.Split(id, "/")
		// If registered, the ID will match the AWS instance ID.
		// If unregistered, the ID will match the machine name.
		if machineID[len(machineID)-1] == nodeID[len(nodeID)-1] ||
			nodeID[len(nodeID)-1] == machine.Name {

			Name = machine.Name
			Namespace = machine.Namespace
			break
		}
	}

	if Name == "" {
		// Could not find any machine corresponds to node %+v", id
		klog.V(4).Infof("No machine found for node ID %q", id)
		return nil, nil
	}
	return &Ref{
		Name:      Name,
		Namespace: Namespace,
	}, nil
}

// MachineDeployment implements NodeGroup interface.
type MachineDeployment struct {
	Ref

	mcmManager *McmManager

	minSize int
	maxSize int
}

// MaxSize returns maximum size of the node group.
func (machinedeployment *MachineDeployment) MaxSize() int {
	return machinedeployment.maxSize
}

// MinSize returns minimum size of the node group.
func (machinedeployment *MachineDeployment) MinSize() int {
	return machinedeployment.minSize
}

// TargetSize returns the current TARGET size of the node group. It is possible that the
// number is different from the number of nodes registered in Kubernetes.
func (machinedeployment *MachineDeployment) TargetSize() (int, error) {
	size, err := machinedeployment.mcmManager.GetMachineDeploymentSize(machinedeployment)
	return int(size), err
}

// Exist checks if the node group really exists on the cloud provider side. Allows to tell the
// theoretical node group from the real one.
// TODO: Implement this to check if machine-deployment really exists.
func (machinedeployment *MachineDeployment) Exist() bool {
	return true
}

// Create creates the node group on the cloud provider side.
func (machinedeployment *MachineDeployment) Create() (cloudprovider.NodeGroup, error) {
	return nil, cloudprovider.ErrAlreadyExist
}

// Autoprovisioned returns true if the node group is autoprovisioned.
func (machinedeployment *MachineDeployment) Autoprovisioned() bool {
	return false
}

// Delete deletes the node group on the cloud provider side.
// This will be executed only for autoprovisioned node groups, once their size drops to 0.
func (machinedeployment *MachineDeployment) Delete() error {
	return cloudprovider.ErrNotImplemented
}

// IncreaseSize of the Machinedeployment.
func (machinedeployment *MachineDeployment) IncreaseSize(delta int) error {
	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}
	size, err := machinedeployment.mcmManager.GetMachineDeploymentSize(machinedeployment)
	if err != nil {
		return err
	}
	if int(size)+delta > machinedeployment.MaxSize() {
		return fmt.Errorf("size increase too large - desired:%d max:%d", int(size)+delta, machinedeployment.MaxSize())
	}
	return machinedeployment.mcmManager.SetMachineDeploymentSize(machinedeployment, size+int64(delta))
}

// DecreaseTargetSize decreases the target size of the node group. This function
// doesn't permit to delete any existing node and can be used only to reduce the
// request for new nodes that have not been yet fulfilled. Delta should be negative.
// It is assumed that cloud provider will not delete the existing nodes if the size
// when there is an option to just decrease the target.
func (machinedeployment *MachineDeployment) DecreaseTargetSize(delta int) error {
	if delta >= 0 {
		return fmt.Errorf("size decrease size must be negative")
	}
	size, err := machinedeployment.mcmManager.GetMachineDeploymentSize(machinedeployment)
	if err != nil {
		return err
	}
	return machinedeployment.mcmManager.SetMachineDeploymentSize(machinedeployment, size+int64(delta))
}

// Belongs returns true if the given node belongs to the NodeGroup.
// TODO: Implement this to iterate over machines under machinedeployment, and return true if node exists in list.
func (machinedeployment *MachineDeployment) Belongs(node *apiv1.Node) (bool, error) {
	ref, err := ReferenceFromProviderID(machinedeployment.mcmManager, node.Spec.ProviderID)
	if err != nil {
		return false, err
	}
	targetMd, err := machinedeployment.mcmManager.GetMachineDeploymentForMachine(ref)
	if err != nil {
		return false, err
	}
	if targetMd == nil {
		return false, fmt.Errorf("%s doesn't belong to a known MachinDeployment", node.Name)
	}
	if targetMd.Id() != machinedeployment.Id() {
		return false, nil
	}
	return true, nil
}

// DeleteNodes deletes the nodes from the group.
func (machinedeployment *MachineDeployment) DeleteNodes(nodes []*apiv1.Node) error {
	size, err := machinedeployment.mcmManager.GetMachineDeploymentSize(machinedeployment)
	if err != nil {
		return err
	}
	if int(size) <= machinedeployment.MinSize() {
		return fmt.Errorf("min size reached, nodes will not be deleted")
	}
	machines := make([]*Ref, 0, len(nodes))
	for _, node := range nodes {
		belongs, err := machinedeployment.Belongs(node)
		if err != nil {
			return err
		} else if !belongs {
			return fmt.Errorf("%s belongs to a different machinedeployment than %s", node.Name, machinedeployment.Id())
		}
		ref, err := ReferenceFromProviderID(machinedeployment.mcmManager, node.Spec.ProviderID)
		if err != nil {
			return fmt.Errorf("Couldn't find the machine-name from provider-id %s", node.Spec.ProviderID)
		}
		machines = append(machines, ref)
	}
	return machinedeployment.mcmManager.DeleteMachines(machines)
}

// Id returns machinedeployment id.
func (machinedeployment *MachineDeployment) Id() string {
	return machinedeployment.Name
}

// Debug returns a debug string for the Asg.
func (machinedeployment *MachineDeployment) Debug() string {
	return fmt.Sprintf("%s (%d:%d)", machinedeployment.Id(), machinedeployment.MinSize(), machinedeployment.MaxSize())
}

// Nodes returns a list of all nodes that belong to this node group.
func (machinedeployment *MachineDeployment) Nodes() ([]cloudprovider.Instance, error) {
	nodeProviderIDs, err := machinedeployment.mcmManager.GetMachineDeploymentNodes(machinedeployment)
	if err != nil {
		return nil, fmt.Errorf("Failed to get the nodes backed by the machinedeployment %q", machinedeployment.Name)
	}

	instances := make([]cloudprovider.Instance, len(nodeProviderIDs))
	for i := range nodeProviderIDs {
		instances[i] = cloudprovider.Instance{
			Id: nodeProviderIDs[i],
		}
	}
	return instances, nil
}

// GetOptions returns NodeGroupAutoscalingOptions that should be used for this particular
// NodeGroup. Returning a nil will result in using default options.
// Implementation optional.
// TODO: add proper implementation
func (machinedeployment *MachineDeployment) GetOptions(defaults config.NodeGroupAutoscalingOptions) (*config.NodeGroupAutoscalingOptions, error) {
	return nil, cloudprovider.ErrNotImplemented
}

// TemplateNodeInfo returns a node template for this node group.
func (machinedeployment *MachineDeployment) TemplateNodeInfo() (*schedulerframework.NodeInfo, error) {

	nodeTemplate, err := machinedeployment.mcmManager.GetMachineDeploymentNodeTemplate(machinedeployment)
	if err != nil {
		return nil, err
	}

	node, err := machinedeployment.mcmManager.buildNodeFromTemplate(machinedeployment.Name, nodeTemplate)
	if err != nil {
		return nil, err
	}

	nodeInfo := schedulerframework.NewNodeInfo(cloudprovider.BuildKubeProxy(machinedeployment.Name))
	nodeInfo.SetNode(node)
	return nodeInfo, nil
}

func buildMachineDeploymentFromSpec(value string, mcmManager *McmManager) (*MachineDeployment, error) {
	spec, err := dynamic.SpecFromString(value, true)

	if err != nil {
		return nil, fmt.Errorf("failed to parse node group spec: %v", err)
	}
	s := strings.Split(spec.Name, ".")
	Namespace, Name := s[0], s[1]

	machinedeployment := buildMachineDeployment(mcmManager, spec.MinSize, spec.MaxSize, Namespace, Name)
	return machinedeployment, nil
}

func buildMachineDeployment(mcmManager *McmManager, minSize int, maxSize int, namespace string, name string) *MachineDeployment {
	return &MachineDeployment{
		mcmManager: mcmManager,
		minSize:    minSize,
		maxSize:    maxSize,
		Ref: Ref{
			Name:      name,
			Namespace: namespace,
		},
	}
}
