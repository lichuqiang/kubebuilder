/*
Copyright 2018 The Kubernetes Authors.

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

package e2e

import (
	"testing"

	"github.com/kubernetes-sigs/kubebuilder/test/e2e/framework"
	"github.com/kubernetes-sigs/kubebuilder/test/e2e/framework/ginkgowrapper"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)


// RunE2ETests checks configuration parameters (specified through flags) and then runs
// E2E tests using the Ginkgo runner.
func RunE2ETests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgowrapper.Fail)
	glog.Infof("Starting kubebuilder suite")
	RunSpecs(t, "Kubebuilder e2e suite")
}

var _ = Describe("Kubebuilder workflow", func() {
	By("init project")
	framework.RunCommandOrDie(framework.KubebuilderCommand, "init", "--domain", "example.com")

	By("creating resource definition")
	framework.RunCommandOrDie(framework.KubebuilderCommand,
		"create", "resource", "--group", "bar", "--version", "alpha1", "--kind", "Foo")

	By("creating core-type resource controller")
	framework.RunCommandOrDie(framework.KubebuilderCommand,
		"create", "controller", "--group", "apps", "--version", "v1beta2", "--kind", "Deployment", "--core-type")

	By("building image")
	imageName := "gcr.io/kubeships/controller-manager:" + framework.NowStamp()
	framework.RunCommandOrDie(framework.DockerCommand,
		"build", framework.TestContext.ProjectDir, "Dockerfile.controller", "-t", imageName)
	defer framework.RunCommandOrDie(framework.DockerCommand, "rmi", "-f", imageName)

	By("creating config")
	framework.RunCommandOrDie(framework.KubebuilderCommand,
		"create", "config", "--controller-image", "imageName", "--name", "kubebar")

	By("installing controller-manager in cluster")
	framework.RunCommandOrDie(framework.KubectlCommand, "apply", "-f", framework.TestContext.ProjectDir+"hack/install.yaml")

	By("creating resource object")
	framework.RunCommandOrDie(framework.KubectlCommand, "create", "-f", framework.TestContext.ProjectDir+"hack/sample/foo.yaml")

})