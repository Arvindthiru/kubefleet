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

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/kubernetes"

	"github.com/kubefleet-dev/kubefleet/test/e2e/framework"
)

// TODO (mng): move this test to join/leave tests after those tests finished.
var _ = Describe("Test metrics exposure", func() {
	It("check exposed metrics on hub cluster", func() {
		By("creating cluster REST config")
		clusterConfig := framework.GetClientConfig(HubCluster)
		restConfig, err := clusterConfig.ClientConfig()
		Expect(err).ToNot(HaveOccurred())

		By("creating cluster clientSet")
		clientSet, err := kubernetes.NewForConfig(restConfig)
		Expect(err).ToNot(HaveOccurred())

		By("getting metrics exposed at /metrics endpoint")
		metrics, err := clientSet.RESTClient().Get().AbsPath("/metrics").DoRaw(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(metrics).ToNot(BeEmpty())
	})

	It("check exposed metrics on member cluster", func() {
		By("creating cluster REST config")
		clusterConfig := framework.GetClientConfig(MemberCluster)
		restConfig, err := clusterConfig.ClientConfig()
		Expect(err).ToNot(HaveOccurred())

		By("creating cluster clientSet")
		clientSet, err := kubernetes.NewForConfig(restConfig)
		Expect(err).ToNot(HaveOccurred())

		By("getting metrics exposed at /metrics endpoint")
		data, err := clientSet.RESTClient().Get().AbsPath("/metrics").DoRaw(context.Background())
		Expect(err).ToNot(HaveOccurred())
		Expect(data).ToNot(BeEmpty())
	})
})
