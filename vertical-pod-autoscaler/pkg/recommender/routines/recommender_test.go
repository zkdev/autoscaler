/*
Copyright 2022 The Kubernetes Authors.

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

package routines

import (
	"testing"
	"time"

	labels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"

	"github.com/stretchr/testify/assert"
)

func TestSortedRecommendation(t *testing.T) {
	cases := []struct {
		name         string
		resources    logic.RecommendedPodResources
		expectedLast []string
	}{
		{
			name: "All recommendations sorted",
			resources: logic.RecommendedPodResources{
				"a-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"b-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"c-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"d-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
			},
			expectedLast: []string{
				"a-container",
				"b-container",
				"c-container",
				"d-container",
			},
		},
		{
			name: "All recommendations unsorted",
			resources: logic.RecommendedPodResources{
				"b-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"a-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"d-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
				"c-container": logic.RecommendedContainerResources{Target: model.Resources{model.ResourceCPU: model.CPUAmountFromCores(1), model.ResourceMemory: model.MemoryAmountFromBytes(1000)}},
			},
			expectedLast: []string{
				"a-container",
				"b-container",
				"c-container",
				"d-container",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			namespace := "test-namespace"
			vpa := model.NewVpa(model.VpaID{Namespace: namespace, VpaName: "my-vpa"}, labels.Nothing(), time.Unix(0, 0))
			vpa.UpdateRecommendation(getCappedRecommendation(vpa.ID, tc.resources, nil))
			// Check that the slice is in the correct order.
			for i := range vpa.Recommendation.ContainerRecommendations {
				assert.Equal(t, tc.expectedLast[i], vpa.Recommendation.ContainerRecommendations[i].ContainerName)
			}
		})
	}
}
