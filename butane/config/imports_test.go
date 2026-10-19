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

package config_test

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coreos/ignition/v2/butane/config"
	"github.com/coreos/ignition/v2/butane/config/common"
	ign34 "github.com/coreos/ignition/v2/config/v3_4"
	ign34types "github.com/coreos/ignition/v2/config/v3_4/types"
	ign36 "github.com/coreos/ignition/v2/config/v3_6"
	ign36types "github.com/coreos/ignition/v2/config/v3_6/types"
	ign37 "github.com/coreos/ignition/v2/config/v3_7_experimental"
	ign37types "github.com/coreos/ignition/v2/config/v3_7_experimental/types"
	"github.com/vincent-petithory/dataurl"
)

type importSpec struct {
	variant      string
	version      string
	childVersion string
	expectedIgn  string
}

func TestInlineButaneImportsForSupportedSpecs(t *testing.T) {
	for _, spec := range []importSpec{
		{variant: "fcos", version: "1.7.0", childVersion: "1.7.0", expectedIgn: "3.6.0"},
		{variant: "fcos", version: "1.8.0-experimental", childVersion: "1.8.0-experimental", expectedIgn: "3.7.0-experimental"},
		{variant: "flatcar", version: "1.1.0", childVersion: "1.1.0", expectedIgn: "3.4.0"},
		{variant: "flatcar", version: "1.2.0-experimental", childVersion: "1.2.0-experimental", expectedIgn: "3.7.0-experimental"},
	} {
		t.Run(spec.variant+"-"+spec.version, func(t *testing.T) {
			payload := strings.Repeat("inline payload ", 99) + "inline payload"
			child := fmt.Sprintf("variant: %s\nversion: %s\nstorage:\n  files:\n    - path: /etc/imported-inline\n      contents:\n        inline: %s\n", spec.variant, spec.childVersion, payload)
			root := fmt.Sprintf("variant: %s\nversion: %s\nignition:\n  config:\n    merge:\n      - inline_butane: |\n%s", spec.variant, spec.version, indent(child, "          "))
			out := translateImport(t, root, t.TempDir(), "")
			assertImportedFile(t, out, spec.expectedIgn, "/etc/imported-inline", payload, true)
		})
	}
}

func TestLocalButaneImportSourceAndFilesDirectories(t *testing.T) {
	rootDir := t.TempDir()
	parentDir := filepath.Join(rootDir, "parents")
	childrenDir := filepath.Join(rootDir, "fragments")
	mustNoError(t, os.MkdirAll(parentDir, 0755))
	mustNoError(t, os.MkdirAll(filepath.Join(childrenDir, "nested"), 0755))
	mustNoError(t, os.MkdirAll(filepath.Join(childrenDir, "custom"), 0755))
	writeTestFile(t, filepath.Join(childrenDir, "default.txt"), "default child directory")
	writeTestFile(t, filepath.Join(childrenDir, "custom", "override.txt"), "relative override directory")
	writeTestFile(t, filepath.Join(childrenDir, "nested", "nested.txt"), "nested child directory")
	writeTestFile(t, filepath.Join(rootDir, "unrelated-files-dir", "default.txt"), "wrong files directory")
	mustNoError(t, os.MkdirAll(filepath.Join(rootDir, "unrelated-files-dir"), 0755))

	child := "variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/default-child-dir\n      contents:\n        local: default.txt\nignition:\n  config:\n    merge:\n      - local_butane:\n          path: nested/grandchild.bu\n"
	nested := "variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/nested-child-dir\n      contents:\n        local: nested.txt\n"
	override := "variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/override-child-dir\n      contents:\n        local: override.txt\n"
	writeTestFile(t, filepath.Join(childrenDir, "default.bu"), child)
	writeTestFile(t, filepath.Join(childrenDir, "nested", "grandchild.bu"), nested)
	writeTestFile(t, filepath.Join(childrenDir, "override.bu"), override)

	root := `variant: fcos
version: 1.7.0
ignition:
  config:
    merge:
      - local_butane:
          path: ../fragments/default.bu
      - local_butane:
          path: ../fragments/override.bu
          files_dir: custom
      - inline_butane: |
          variant: fcos
          version: 1.7.0
          ignition:
            config:
              merge:
                - local_butane:
                    path: ../fragments/inline-child.bu
`
	inlineChild := "variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/inline-source-dir\n      contents:\n        inline: inherited source directory\n"
	writeTestFile(t, filepath.Join(childrenDir, "inline-child.bu"), inlineChild)

	out := translateImport(t, root, parentDir, filepath.Join(rootDir, "unrelated-files-dir"))
	merged := expandIgnition36(t, out)
	mustEqual(t, "3.6.0", merged.Ignition.Version)
	assertFileContents(t, merged.Storage.Files, "/etc/default-child-dir", "default child directory", false)
	assertFileContents(t, merged.Storage.Files, "/etc/override-child-dir", "relative override directory", false)
	assertFileContents(t, merged.Storage.Files, "/etc/nested-child-dir", "nested child directory", false)
	assertFileContents(t, merged.Storage.Files, "/etc/inline-source-dir", "inherited source directory", false)
}

func TestSourceDirDoesNotChangeOrdinaryLocalFileResolution(t *testing.T) {
	sourceDir := t.TempDir()
	filesDir := t.TempDir()
	writeTestFile(t, filepath.Join(sourceDir, "payload.txt"), "source directory decoy")
	writeTestFile(t, filepath.Join(filesDir, "payload.txt"), "explicit files directory")
	input := []byte("variant: fcos\nversion: 1.7.0\nstorage:\n  files:\n    - path: /etc/local\n      contents:\n        local: payload.txt\n")

	_, report, err := config.TranslateBytes(input, common.TranslateBytesOptions{
		TranslateOptions: common.TranslateOptions{SourceDir: sourceDir},
	})
	mustError(t, err)
	if !strings.Contains(report.String(), common.ErrNoFilesDir.Error()) {
		t.Fatalf("expected missing-files-dir error, got report: %s", report.String())
	}

	output := translateImport(t, string(input), sourceDir, filesDir)
	merged := expandIgnition36(t, output)
	assertFileContents(t, merged.Storage.Files, "/etc/local", "explicit files directory", false)

	inlineRoot := "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - inline_butane: |\n          variant: fcos\n          version: 1.7.0\n          storage:\n            files:\n              - path: /etc/inline-local\n                contents:\n                  local: payload.txt\n"
	_, report, err = config.TranslateBytes([]byte(inlineRoot), common.TranslateBytesOptions{
		TranslateOptions: common.TranslateOptions{SourceDir: sourceDir},
	})
	mustError(t, err)
	if !strings.Contains(report.String(), common.ErrNoFilesDir.Error()) {
		t.Fatalf("expected inline-import missing-files-dir error, got report: %s", report.String())
	}
}

func TestImportValidationErrors(t *testing.T) {
	validChild := "variant: fcos\nversion: 1.7.0\n"
	tests := []struct {
		name string
		root string
	}{
		{
			name: "missing child file",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - local_butane:\n          path: missing.bu\n",
		},
		{
			name: "malformed child",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - inline_butane: |\n          variant: fcos\n          version: [\n",
		},
		{
			name: "variant mismatch",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - inline_butane: |\n          variant: flatcar\n          version: 1.7.0\n",
		},
		{
			name: "version mismatch",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - inline_butane: |\n          variant: fcos\n          version: 1.8.0-experimental\n",
		},
		{
			name: "conflicting child sources",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - local_butane:\n          path: child.bu\n        inline_butane: |\n" + indent(validChild, "          "),
		},
		{
			name: "missing local child path",
			root: "variant: fcos\nversion: 1.7.0\nignition:\n  config:\n    merge:\n      - local_butane: {}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "child.bu"), validChild)
			_, _, err := config.TranslateBytes([]byte(tt.root), common.TranslateBytesOptions{
				TranslateOptions: common.TranslateOptions{SourceDir: dir},
			})
			mustError(t, err)
		})
	}
}

func translateImport(t *testing.T, input, sourceDir, filesDir string) []byte {
	t.Helper()
	out, _, err := config.TranslateBytes([]byte(input), common.TranslateBytesOptions{
		Raw: true,
		TranslateOptions: common.TranslateOptions{
			SourceDir: sourceDir,
			FilesDir:  filesDir,
		},
	})
	mustNoError(t, err)
	return out
}

func assertImportedFile(t *testing.T, raw []byte, ignitionVersion, path, want string, wantGzip bool) {
	t.Helper()
	switch ignitionVersion {
	case "3.6.0":
		cfg := expandIgnition36(t, raw)
		mustEqual(t, ignitionVersion, cfg.Ignition.Version)
		assertFileContents(t, cfg.Storage.Files, path, want, wantGzip)
	case "3.4.0":
		cfg := expandIgnition34(t, raw)
		mustEqual(t, ignitionVersion, cfg.Ignition.Version)
		assertFileContents34(t, cfg.Storage.Files, path, want, wantGzip)
	case "3.7.0-experimental":
		cfg := expandIgnition37(t, raw)
		mustEqual(t, ignitionVersion, cfg.Ignition.Version)
		assertFileContents37(t, cfg.Storage.Files, path, want, wantGzip)
	default:
		t.Fatalf("unexpected Ignition version %q", ignitionVersion)
	}
}

func expandIgnition34(t *testing.T, raw []byte) ign34types.Config {
	t.Helper()
	cfg, _, err := ign34.Parse(raw)
	mustNoError(t, err)
	for _, resource := range cfg.Ignition.Config.Merge {
		if resource.Source == nil {
			continue
		}
		child := expandIgnition34(t, decodeResource(t, *resource.Source, resource.Compression))
		cfg = ign34.Merge(cfg, child)
	}
	return cfg
}

func expandIgnition36(t *testing.T, raw []byte) ign36types.Config {
	t.Helper()
	cfg, _, err := ign36.Parse(raw)
	mustNoError(t, err)
	for _, resource := range cfg.Ignition.Config.Merge {
		if resource.Source == nil {
			continue
		}
		child := expandIgnition36(t, decodeResource(t, *resource.Source, resource.Compression))
		cfg = ign36.Merge(cfg, child)
	}
	return cfg
}

func expandIgnition37(t *testing.T, raw []byte) ign37types.Config {
	t.Helper()
	cfg, _, err := ign37.Parse(raw)
	mustNoError(t, err)
	for _, resource := range cfg.Ignition.Config.Merge {
		if resource.Source == nil {
			continue
		}
		child := expandIgnition37(t, decodeResource(t, *resource.Source, resource.Compression))
		cfg = ign37.Merge(cfg, child)
	}
	return cfg
}

func decodeResource(t *testing.T, source string, compression *string) []byte {
	t.Helper()
	data, err := dataurl.DecodeString(source)
	mustNoError(t, err)
	if compression == nil || *compression != "gzip" {
		return data.Data
	}
	reader, err := gzip.NewReader(bytes.NewReader(data.Data))
	mustNoError(t, err)
	defer reader.Close()
	contents, err := io.ReadAll(reader)
	mustNoError(t, err)
	return contents
}

func assertFileContents(t *testing.T, files []ign36types.File, path, want string, wantGzip bool) {
	t.Helper()
	for _, file := range files {
		if file.Node.Path != path || file.Contents.Source == nil {
			continue
		}
		assertCompression(t, file.Contents.Compression, wantGzip, path)
		contents := decodeResource(t, *file.Contents.Source, file.Contents.Compression)
		if string(contents) != want {
			t.Fatalf("unexpected contents for %s: got %q, want %q", path, contents, want)
		}
		return
	}
	t.Fatalf("Ignition file %q not found", path)
}

func assertFileContents34(t *testing.T, files []ign34types.File, path, want string, wantGzip bool) {
	t.Helper()
	for _, file := range files {
		if file.Node.Path != path || file.Contents.Source == nil {
			continue
		}
		assertCompression(t, file.Contents.Compression, wantGzip, path)
		contents := decodeResource(t, *file.Contents.Source, file.Contents.Compression)
		if string(contents) != want {
			t.Fatalf("unexpected contents for %s: got %q, want %q", path, contents, want)
		}
		return
	}
	t.Fatalf("Ignition file %q not found", path)
}

func assertFileContents37(t *testing.T, files []ign37types.File, path, want string, wantGzip bool) {
	t.Helper()
	for _, file := range files {
		if file.Node.Path != path || file.Contents.Source == nil {
			continue
		}
		assertCompression(t, file.Contents.Compression, wantGzip, path)
		contents := decodeResource(t, *file.Contents.Source, file.Contents.Compression)
		if string(contents) != want {
			t.Fatalf("unexpected contents for %s: got %q, want %q", path, contents, want)
		}
		return
	}
	t.Fatalf("Ignition file %q not found", path)
}

func assertCompression(t *testing.T, compression *string, wantGzip bool, path string) {
	t.Helper()
	if wantGzip {
		if compression == nil || *compression != "gzip" {
			t.Fatalf("expected gzip compression for %s, got %v", path, compression)
		}
		return
	}
	if compression != nil && *compression == "gzip" {
		t.Fatalf("unexpected gzip compression for %s", path)
	}
}

func indent(input, prefix string) string {
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(input, "\n"), "\n") {
		out.WriteString(prefix)
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	mustNoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	mustNoError(t, os.WriteFile(path, []byte(contents), 0644))
}

func mustNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func mustError(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
}

func mustEqual(t testing.TB, want, got string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
