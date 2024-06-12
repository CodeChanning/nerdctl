/*
   Copyright The containerd Authors.

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
	"bytes"
	"fmt"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/nerdctl/v2/pkg/testutil"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"
)

// skipAttachForDocker should be called by attach-related tests that assert 'read detach keys' in stdout.
func skipAttachForDocker(t *testing.T) {
	t.Helper()
	if testutil.GetTarget() == testutil.Docker {
		t.Skip("When detaching from a container, for a session started with 'docker attach'" +
			", it prints 'read escape sequence', but for one started with 'docker (run|start)', it prints nothing." +
			" However, the flag is called '--detach-keys' in all cases" +
			", so nerdctl prints 'read detach keys' for all cases" +
			", and that's why this test is skipped for Docker.")
	}
}

// prepareContainerToAttach spins up a container (entrypoint = shell) with `-it` and detaches from it
// so that it can be re-attached to later.
func prepareContainerToAttach(base *testutil.Base, containerName string) {
	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
			[]byte{16, 17}, // ctrl+p,ctrl+q, see https://www.physics.udel.edu/~watson/scen103/ascii.html
		))),
	}
	// unbuffer(1) emulates tty, which is required by `nerdctl run -t`.
	// unbuffer(1) can be installed with `apt-get install expect`.
	//
	// "-p" is needed because we need unbuffer to read from stdin, and from [1]:
	// "Normally, unbuffer does not read from stdin. This simplifies use of unbuffer in some situations.
	//  To use unbuffer in a pipeline, use the -p flag."
	//
	// [1] https://linux.die.net/man/1/unbuffer
	base.CmdWithHelper([]string{"unbuffer", "-p"}, "run", "-it", "--name", containerName, testutil.CommonImage).
		CmdOption(opts...).AssertOutContains("read detach keys")
	container := base.InspectContainer(containerName)
	assert.Equal(base.T, container.State.Running, true)
}

// prepareContainerToAttach spins up a container (entrypoint = shell) with `-it` and detaches from it
// so that it can be re-attached to later.
func prepareContainerToAttachWithScript(base *testutil.Base, containerName string, testScript string) *icmd.Result {
	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
			[]byte{16, 17}, // ctrl+p,ctrl+q, see https://www.physics.udel.edu/~watson/scen103/ascii.html
		))),
	}
	// unbuffer(1) emulates tty, which is required by `nerdctl run -t`.
	// unbuffer(1) can be installed with `apt-get install expect`.
	//
	// "-p" is needed because we need unbuffer to read from stdin, and from [1]:
	// "Normally, unbuffer does not read from stdin. This simplifies use of unbuffer in some situations.
	//  To use unbuffer in a pipeline, use the -p flag."
	//
	// [1] https://linux.die.net/man/1/unbuffer
	proc := base.Cmd("run", "-i", "--name", containerName, testutil.CommonImage, "sh", "-c", testScript).
		CmdOption(opts...).Run()
	fmt.Print(proc.Combined())
	container := base.InspectContainer(containerName)
	assert.Equal(base.T, container.State.Running, true)
	return proc
}

func TestAttach(t *testing.T) {
	t.Parallel()

	skipAttachForDocker(t)

	base := testutil.NewBase(t)
	containerName := testutil.Identifier(t)

	defer base.Cmd("container", "rm", "-f", containerName).AssertOK()
	prepareContainerToAttach(base, containerName)

	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(strings.NewReader("expr 1 + 1\nexit\n"))),
	}
	// `unbuffer -p` returns 0 even if the underlying nerdctl process returns a non-zero exit code,
	// so the exit code cannot be easily tested here.
	base.CmdWithHelper([]string{"unbuffer", "-p"}, "attach", containerName).CmdOption(opts...).AssertOutContains("2")
	container := base.InspectContainer(containerName)
	assert.Equal(base.T, container.State.Running, false)
}

func TestAttachDetachKeys(t *testing.T) {
	t.Parallel()

	skipAttachForDocker(t)

	base := testutil.NewBase(t)
	containerName := testutil.Identifier(t)

	defer base.Cmd("container", "rm", "-f", containerName).AssertOK()
	prepareContainerToAttach(base, containerName)

	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
			[]byte{1, 2}, // https://www.physics.udel.edu/~watson/scen103/ascii.html
		))),
	}
	base.CmdWithHelper([]string{"unbuffer", "-p"}, "attach", "--detach-keys=ctrl-a,ctrl-b", containerName).
		CmdOption(opts...).AssertOutContains("read detach keys")
	container := base.InspectContainer(containerName)
	assert.Equal(base.T, container.State.Running, true)
}

// func TestAttachSigProxy(t *testing.T) {
// 	t.Parallel()

// 	skipAttachForDocker(t)

// 	base := testutil.NewBase(t)
// 	containerName := testutil.Identifier(t)

// 	defer base.Cmd("container", "rm", "-f", containerName).AssertOK()
// 	prepareContainerToAttach(base, containerName)

// 	opts := []func(*testutil.Cmd){
// 		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
// 			[]byte{3}, // https://www.physics.udel.edu/~watson/scen103/ascii.html
// 		))),
// 	}
// 	base.Cmd("attach", "--sig-proxy=true", containerName).
// 		CmdOption(opts...).AssertOutContains("read detach keys")
// 	container := base.InspectContainer(containerName)
// 	assert.Equal(base.T, container.State.Running, true)
// }

func TestAttachSigProxyDefault(t *testing.T) {
	t.Parallel()
	base := testutil.NewBase(t)
	testContainerName := testutil.Identifier(t)
	defer base.Cmd("rm", "-f", testContainerName).Run()

	process1 := base.Cmd("run", "--name", testContainerName, testutil.CommonImage, "sh", "-c", testutil.SigProxyTestScript).Start()

	// This sleep waits for until we reach the trap command in the shell script, if sigint is send before that we dont enter the while loop.
	time.Sleep(1 * time.Second)
	process2 := base.Cmd("attach", testContainerName).Start()

	// Waiting for attachment.
	time.Sleep(1 * time.Second)

	syscall.Kill(process2.Cmd.Process.Pid, syscall.SIGINT)
	process2.Cmd.Wait()
	process1.Cmd.Wait()
	combinedOut := process1.Combined() + process2.Combined()

	assert.Assert(base.T, strings.Contains(combinedOut, "got sigint"), fmt.Sprintf("expected output to contain %q: %q", "got sigint", combinedOut))
}

// func TestAttachSigProxyTrue(t *testing.T) {
// 	t.Parallel()
// 	base := testutil.NewBase(t)
// 	testContainerName := testutil.Identifier(t)
// 	defer base.Cmd("rm", "-f", testContainerName).Run()

// 	process1 := base.Cmd("run", "--name", testContainerName, testutil.CommonImage, "sh", "-c", testutil.SigProxyTestScript).Start()

// 	// This sleep waits for until we reach the trap command in the shell script, if sigint is send before that we dont enter the while loop.
// 	time.Sleep(1 * time.Second)
// 	process2 := base.Cmd("attach", "--sig-proxy=true", testContainerName).Start()

// 	// Waiting for attachment.
// 	time.Sleep(1 * time.Second)

// 	syscall.Kill(process2.Cmd.Process.Pid, syscall.SIGINT)
// 	process2.Cmd.Wait()
// 	process1.Cmd.Wait()
// 	combinedOut := process1.Combined() + process2.Combined()

// 	assert.Assert(base.T, strings.Contains(combinedOut, "got sigint"), fmt.Sprintf("expected output to contain %q: %q", "got sigint", combinedOut))
// }

func TestAttachSigProxyTrue(t *testing.T) {
	t.Parallel()
	base := testutil.NewBase(t)
	containerName := testutil.Identifier(t)
	defer base.Cmd("rm", "-f", containerName).Run()

	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
			[]byte{3}, // ctrl+p,ctrl+q, see https://www.physics.udel.edu/~watson/scen103/ascii.html
		))),
	}

	runProc := prepareContainerToAttachWithScript(base, containerName, testutil.SigProxyTestScript)

	// This sleep waits for until we reach the trap command in the shell script, if sigint is send before that we don't enter the while loop.
	time.Sleep(1 * time.Second)
	attachProc := base.Cmd("attach", "--sig-proxy=true", containerName).CmdOption(opts...).Start()

	// Waiting for attachment.
	time.Sleep(1 * time.Second)

	syscall.Kill(attachProc.Cmd.Process.Pid, syscall.SIGINT)
	fmt.Print("sent signal !!!!\n")
	attachProc.Cmd.Wait()

	combinedOut := attachProc.Combined() + runProc.Combined()

	assert.Assert(base.T, strings.Contains(combinedOut, "got sigint"), fmt.Sprintf("expected output to contain %q: %q", "got sigint", combinedOut))
}

func TestAttachSigProxyFalse(t *testing.T) {
	t.Parallel()
	base := testutil.NewBase(t)
	containerName := testutil.Identifier(t)
	defer base.Cmd("rm", "-f", containerName).Run()

	runProc := prepareContainerToAttachWithScript(base, containerName, testutil.SigProxyTestScript)

	opts := []func(*testutil.Cmd){
		testutil.WithStdin(testutil.NewDelayOnceReader(bytes.NewReader(
			[]byte{3}, // ctrl+p,ctrl+q, see https://www.physics.udel.edu/~watson/scen103/ascii.html
		))),
	}

	// This sleep waits for until we reach the trap command in the shell script, if sigint is send before that we dont enter the while loop.
	time.Sleep(1 * time.Second)
	attachProc := base.Cmd("attach", "--sig-proxy=false", containerName).CmdOption(opts...).Run()

	// Waiting for attachment.
	time.Sleep(1 * time.Second)

	syscall.Kill(attachProc.Cmd.Process.Pid, syscall.SIGINT)
	attachProc.Cmd.Wait()

	combinedOut := attachProc.Combined() + runProc.Combined()
	fmt.Print("flase combined out: " + combinedOut + "\n")
	assert.Assert(base.T, !strings.Contains(combinedOut, "got sigint"), fmt.Sprintf("expected output to contain %q: %q", "got sigint", combinedOut))
}
