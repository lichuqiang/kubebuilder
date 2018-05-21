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

package framework

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/kubernetes-sigs/kubebuilder/test/e2e/framework/ginkgowrapper"

	"k8s.io/client-go/tools/clientcmd"
)

const (
	KubectlCommand = "kubectl-command"
	KubebuilderCommand = "kubebuilder-command"
	DockerCommand = "docker-command"
)

// Code originally copied from kubernetes/kubernetes at
// https://github.com/kubernetes/kubernetes/blob/master/test/e2e/framework/util.go

// KubectlCmd runs the kubectl executable through the wrapper script.
func KubectlCmd(args ...string) *exec.Cmd {
	defaultArgs := []string{}

	// Reference a --server option so tests can run anywhere.
	if TestContext.Host != "" {
		defaultArgs = append(defaultArgs, "--"+clientcmd.FlagAPIServer+"="+TestContext.Host)
	}
	if TestContext.KubeConfig != "" {
		defaultArgs = append(defaultArgs, "--"+clientcmd.RecommendedConfigPathFlag+"="+TestContext.KubeConfig)

		// Reference the KubeContext
		if TestContext.KubeContext != "" {
			defaultArgs = append(defaultArgs, "--"+clientcmd.FlagContext+"="+TestContext.KubeContext)
		}

	} else {
		if TestContext.CertDir != "" {
			defaultArgs = append(defaultArgs,
				fmt.Sprintf("--certificate-authority=%s", filepath.Join(TestContext.CertDir, "ca.crt")),
				fmt.Sprintf("--client-certificate=%s", filepath.Join(TestContext.CertDir, "kubecfg.crt")),
				fmt.Sprintf("--client-key=%s", filepath.Join(TestContext.CertDir, "kubecfg.key")))
		}
	}
	kubectlArgs := append(defaultArgs, args...)

	//We allow users to specify path to kubectl, so you can test either "kubectl" or "cluster/kubectl.sh"
	//and so on.
	cmd := exec.Command(TestContext.KubectlPath, kubectlArgs...)

	//caller will invoke this and wait on it.
	return cmd
}

// KubebuilderCmd runs the kubebuilder executable through the wrapper script.
func KubebuilderCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(TestContext.KubebuilderPath, args...)
	// Set projectDir as the work path of kubebuilder
	cmd.Path = TestContext.ProjectDir

	//caller will invoke this and wait on it.
	return cmd
}

// CommandBuilder is used to build, customize and execute a Command.
// Add more functions to customize the builder as needed.
type CommandBuilder struct {
	cmdType string
	cmd     *exec.Cmd
	timeout <-chan time.Time
}

func NewCommand(cmdType string, args ...string) *CommandBuilder {
	b := new(CommandBuilder)
	b.cmdType = cmdType
	switch cmdType {
	case KubectlCommand:
		b.cmd = KubectlCmd(args...)
	case KubebuilderCommand:
		b.cmd = KubebuilderCmd(args...)
	case DockerCommand:
		b.cmd = exec.Command("docker", args...)
	default:
		Failf("Invalid command type: %s", cmdType)
	}
	return b
}

func (b *CommandBuilder) WithEnv(env []string) *CommandBuilder {
	b.cmd.Env = env
	return b
}

func (b *CommandBuilder) WithTimeout(t <-chan time.Time) *CommandBuilder {
	b.timeout = t
	return b
}

func (b CommandBuilder) WithStdinData(data string) *CommandBuilder {
	b.cmd.Stdin = strings.NewReader(data)
	return &b
}

func (b CommandBuilder) WithStdinReader(reader io.Reader) *CommandBuilder {
	b.cmd.Stdin = reader
	return &b
}

func (b CommandBuilder) ExecOrDie() string {
	str, err := b.Exec()

	if isTimeout(err) {
		Logf("Hit i/o timeout error.")
		if b.cmdType == KubectlCommand {
			// In case of i/o timeout error, try talking to the apiserver again after 2s before dying.
			// Note that we're still dying after retrying so that we can get visibility to triage it further.
			Logf("Talking to the server 2s later to see if it's temporary.")
			time.Sleep(2 * time.Second)
			retryStr, retryErr := RunCommand(KubectlCommand, "version")
			Logf("stdout: %q", retryStr)
			Logf("err: %v", retryErr)
		}
	}
	Expect(err).NotTo(HaveOccurred())
	return str
}

func isTimeout(err error) bool {
	switch err := err.(type) {
	case net.Error:
		if err.Timeout() {
			return true
		}
	case *url.Error:
		if err, ok := err.Err.(net.Error); ok && err.Timeout() {
			return true
		}
	}
	return false
}

func (b CommandBuilder) Exec() (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := b.cmd
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	Logf("Running '%s %s'", cmd.Path, strings.Join(cmd.Args[1:], " ")) // skip arg[0] as it is printed separately
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("error starting %v:\nCommand stdout:\n%v\nstderr:\n%v\nerror:\n%v\n", cmd, cmd.Stdout, cmd.Stderr, err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()
	select {
	case err := <-errCh:
		if err != nil {
			var rc int = 127
			if ee, ok := err.(*exec.ExitError); ok {
				rc = int(ee.Sys().(syscall.WaitStatus).ExitStatus())
				Logf("rc: %d", rc)
			}
			return "", fmt.Errorf("error running %v:\nCommand stdout:\n%v\nstderr:\n%v\nerror:\n%v\ncode: %d", cmd, cmd.Stdout, cmd.Stderr, err, rc)
		}
	case <-b.timeout:
		b.cmd.Process.Kill()
		return "", fmt.Errorf("timed out waiting for command %v:\nCommand stdout:\n%v\nstderr:\n%v\n", cmd, cmd.Stdout, cmd.Stderr)
	}
	Logf("stderr: %q", stderr.String())
	Logf("stdout: %q", stdout.String())
	return stdout.String(), nil
}

// RunCommandOrDie is a convenience wrapper over underlying command
func RunCommandOrDie(cmdType string, args ...string) string {
	return NewCommand(cmdType, args...).ExecOrDie()
}

// RunCommand is a convenience wrapper over underlying command
func RunCommand(cmdType string, args ...string) (string, error) {
	return NewCommand(cmdType, args...).Exec()
}

// RunCommandOrDieInput is a convenience wrapper over underlying command that takes input to stdin
func RunCommandOrDieInput(cmdType string, data string, args ...string) string {
	return NewCommand(cmdType, args...).WithStdinData(data).ExecOrDie()
}

func NowStamp() string {
	return time.Now().Format(time.StampMilli)
}

func log(level string, format string, args ...interface{}) {
	fmt.Fprintf(GinkgoWriter, NowStamp()+": "+level+": "+format+"\n", args...)
}

func Logf(format string, args ...interface{}) {
	log("INFO", format, args...)
}

func Failf(format string, args ...interface{}) {
	FailfWithOffset(1, format, args...)
}

// FailfWithOffset calls "Fail" and logs the error at "offset" levels above its caller
// (for example, for call chain f -> g -> FailfWithOffset(1, ...) error would be logged for "f").
func FailfWithOffset(offset int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log("INFO", msg)
	ginkgowrapper.Fail(NowStamp()+": "+msg, 1+offset)
}

func Skipf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log("INFO", msg)
	ginkgowrapper.Skip(NowStamp() + ": " + msg)
}
