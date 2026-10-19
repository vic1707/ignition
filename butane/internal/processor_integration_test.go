// Copyright 2026 Red Hat, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileProcessorCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the processor fixtures use POSIX shell scripts")
	}

	binDir := t.TempDir()
	butane := filepath.Join(binDir, "butane")
	build := exec.Command("go", "build", "-o", butane, ".")
	buildOutput, err := build.CombinedOutput()
	mustNoError(t, err, fmt.Sprintf("building Butane CLI: %s", buildOutput))

	processor := filepath.Join(binDir, "processor")
	writeProcessor(t, processor, "#!/bin/sh\nsed 's/processor-placeholder/processor-rendered/g'\n")
	failingProcessor := filepath.Join(binDir, "failing-processor")
	writeProcessor(t, failingProcessor, "#!/bin/sh\ncat >/dev/null\necho processor-failed >&2\nexit 23\n")

	t.Run("processes named input when suffix is allowed", func(t *testing.T) {
		input := filepath.Join(binDir, "root.bu")
		writeProcessorInput(t, input, "processor-placeholder")
		output, err := runButane(butane, nil, "--raw", "--file-processor", processor, "--file-processor-allowed-ext=bu", input)
		mustNoError(t, err, string(output))
		mustContain(t, string(output), "processor-rendered")
		mustNotContain(t, string(output), "processor-placeholder")
	})

	t.Run("bypasses a named input with an unlisted suffix", func(t *testing.T) {
		input := filepath.Join(binDir, "root.yaml")
		writeProcessorInput(t, input, "processor-placeholder")
		output, err := runButane(butane, nil, "--raw", "--file-processor", processor, "--file-processor-allowed-ext=bu", input)
		mustNoError(t, err, string(output))
		mustContain(t, string(output), "processor-placeholder")
		mustNotContain(t, string(output), "processor-rendered")
	})

	t.Run("stdin is processed only when explicitly allowed", func(t *testing.T) {
		input := []byte(processorInput("processor-placeholder"))
		plain, err := runButane(butane, input, "--raw", "--file-processor", processor, "--file-processor-allowed-ext=bu")
		mustNoError(t, err, string(plain))
		mustContain(t, string(plain), "processor-placeholder")
		processed, err := runButane(butane, input, "--raw", "--file-processor", processor, "--file-processor-allowed-ext=bu,stdin")
		mustNoError(t, err, string(processed))
		mustContain(t, string(processed), "processor-rendered")
		mustNotContain(t, string(processed), "processor-placeholder")
	})

	t.Run("an explicitly empty allowlist disables processing", func(t *testing.T) {
		input := filepath.Join(binDir, "empty-list.bu")
		writeProcessorInput(t, input, "processor-placeholder")
		output, err := runButane(butane, nil, "--raw", "--file-processor", processor, "--file-processor-allowed-ext=", input)
		mustNoError(t, err, string(output))
		mustContain(t, string(output), "processor-placeholder")
		mustNotContain(t, string(output), "processor-rendered")
	})

	t.Run("fails when the processor exits unsuccessfully", func(t *testing.T) {
		input := filepath.Join(binDir, "failure.bu")
		writeProcessorInput(t, input, "processor-placeholder")
		output, err := runButane(butane, nil, "--raw", "--file-processor", failingProcessor, "--file-processor-allowed-ext=bu", input)
		mustError(t, err)
		mustContain(t, string(output), "file processor")
		mustContain(t, string(output), "processor-failed")
	})

	t.Run("processes selected local file references", func(t *testing.T) {
		filesDir := filepath.Join(binDir, "files")
		mustNoError(t, os.MkdirAll(filesDir, 0755), "creating local files directory")
		writeProcessor(t, filepath.Join(filesDir, "payload.txt"), "processor-placeholder\n")
		input := filepath.Join(binDir, "local.bu")
		writeProcessorInputWithLocal(t, input)
		output, err := runButane(butane, nil, "--raw", "--files-dir", filesDir, "--file-processor", processor, "--file-processor-allowed-ext=bu,txt", input)
		mustNoError(t, err, string(output))
		mustContain(t, string(output), "processor-rendered")
		mustNotContain(t, string(output), "processor-placeholder")
	})

	t.Run("fails when processor is configured without an executable", func(t *testing.T) {
		input := filepath.Join(binDir, "missing-executable.bu")
		writeProcessorInput(t, input, "processor-placeholder")
		output, err := runButane(butane, nil, "--raw", "--file-processor", filepath.Join(binDir, "missing"), "--file-processor-allowed-ext=bu", input)
		mustError(t, err)
		mustContain(t, string(output), "file processor")
	})
}

func runButane(binary string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command(binary, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	return cmd.CombinedOutput()
}

func processorInput(value string) string {
	return fmt.Sprintf("variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/processor\n      contents:\n        inline: %s\n", value)
}

func writeProcessorInput(t *testing.T, path, value string) {
	t.Helper()
	mustNoError(t, os.WriteFile(path, []byte(processorInput(value)), 0644), "writing test input")
}

func writeProcessorInputWithLocal(t *testing.T, path string) {
	t.Helper()
	input := `variant: fcos
version: 1.7.0
storage:
  files:
    - path: /etc/processor
      contents:
        local: payload.txt
`
	mustNoError(t, os.WriteFile(path, []byte(input), 0644), "writing test input")
}

func writeProcessor(t *testing.T, path, contents string) {
	t.Helper()
	mustNoError(t, os.WriteFile(path, []byte(contents), 0755), "writing test processor")
	mustNoError(t, os.Chmod(path, 0755), "making test processor executable")
}

func mustNoError(t testing.TB, err error, context ...string) {
	t.Helper()
	if err != nil {
		if len(context) > 0 {
			t.Fatalf("%s: %v", context[0], err)
		}
		t.Fatal(err)
	}
}

func mustError(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
}

func mustContain(t testing.TB, value, substring string) {
	t.Helper()
	if !bytes.Contains([]byte(value), []byte(substring)) {
		t.Fatalf("expected output to contain %q, got %q", substring, value)
	}
}

func mustNotContain(t testing.TB, value, substring string) {
	t.Helper()
	if bytes.Contains([]byte(value), []byte(substring)) {
		t.Fatalf("expected output not to contain %q, got %q", substring, value)
	}
}
