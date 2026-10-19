// Copyright 2026 Red Hat, Inc.
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

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coreos/ignition/v2/butane/config/common"
	"github.com/coreos/vcontext/report"
	"github.com/stretchr/testify/assert"
)

const (
	dispatchSourceTestVariant    = "dispatch-source-test"
	dispatchRecursiveTestVariant = "dispatch-recursive-test"
	dispatchVersionTestVariant   = "dispatch-version-test"
	dispatchVariantTestVariant   = "dispatch-variant-test"
	dispatchDefaultTestVariant   = "dispatch-default-test"
	dispatchSemverTestVariant    = "dispatch-semver-test"
)

func TestTranslateBytesDispatchesAndNormalizesSourceDir(t *testing.T) {
	var receivedOptions common.TranslateBytesOptions
	common.RegisterTranslator(dispatchSourceTestVariant, "1.0.0", func(_ []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		receivedOptions = options
		return []byte("translated"), report.Report{}, nil
	})

	got, _, err := TranslateBytes([]byte("variant: "+dispatchSourceTestVariant+"\nversion: 1.0.0\n"), common.TranslateBytesOptions{TranslateOptions: common.TranslateOptions{SourceDir: "."}})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, []byte("translated"), got)
	wantSourceDir, err := filepath.Abs(".")
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, wantSourceDir, receivedOptions.SourceDir)
}

func TestTranslateBytesRecursiveDispatchKeepsVersion(t *testing.T) {
	common.RegisterTranslator(dispatchRecursiveTestVariant, "1.0.0", func(input []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		if bytes.Contains(input, []byte("child: true")) {
			return []byte("child"), report.Report{}, nil
		}
		return common.TranslateBytes([]byte("variant: "+dispatchRecursiveTestVariant+"\nversion: 1.0.0\nchild: true\n"), options)
	})

	got, _, err := common.TranslateBytes([]byte("variant: "+dispatchRecursiveTestVariant+"\nversion: 1.0.0\n"), common.TranslateBytesOptions{})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, []byte("child"), got)
}

func TestTranslateBytesRecursiveDispatchRejectsDifferentVersion(t *testing.T) {
	common.RegisterTranslator(dispatchVersionTestVariant, "1.0.0", func(_ []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		return common.TranslateBytes([]byte("variant: "+dispatchVersionTestVariant+"\nversion: 2.0.0\n"), options)
	})

	_, _, err := common.TranslateBytes([]byte("variant: "+dispatchVersionTestVariant+"\nversion: 1.0.0\n"), common.TranslateBytesOptions{})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), `must use variant "dispatch-version-test" and version "1.0.0"`)
		assert.Contains(t, err.Error(), `got variant "dispatch-version-test" and version "2.0.0"`)
	}
}

func TestTranslateBytesRecursiveDispatchComparesSemanticVersions(t *testing.T) {
	translator := func(input []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		if bytes.Contains(input, []byte("child: true")) {
			return []byte("child"), report.Report{}, nil
		}
		return common.TranslateBytes([]byte("variant: "+dispatchSemverTestVariant+"\nversion: 1.0.0+child\nchild: true\n"), options)
	}
	common.RegisterTranslator(dispatchSemverTestVariant, "1.0.0+parent", translator)
	common.RegisterTranslator(dispatchSemverTestVariant, "1.0.0+child", translator)

	got, _, err := common.TranslateBytes([]byte("variant: "+dispatchSemverTestVariant+"\nversion: 1.0.0+parent\n"), common.TranslateBytesOptions{})
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, []byte("child"), got)
}

func TestTranslateBytesRecursiveDispatchRejectsDifferentVariant(t *testing.T) {
	common.RegisterTranslator(dispatchVariantTestVariant, "1.0.0", func(_ []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		return common.TranslateBytes([]byte("variant: other-dispatch-test\nversion: 1.0.0\n"), options)
	})

	_, _, err := common.TranslateBytes([]byte("variant: "+dispatchVariantTestVariant+"\nversion: 1.0.0\n"), common.TranslateBytesOptions{})
	if assert.Error(t, err) {
		assert.True(t, strings.Contains(err.Error(), `got variant "other-dispatch-test"`))
	}
}

func TestTranslateBytesDefaultsSourceDirToWorkingDirectory(t *testing.T) {
	var sourceDir string
	common.RegisterTranslator(dispatchDefaultTestVariant, "1.0.0", func(_ []byte, options common.TranslateBytesOptions) ([]byte, report.Report, error) {
		sourceDir = options.SourceDir
		return nil, report.Report{}, nil
	})

	_, _, err := common.TranslateBytes([]byte("variant: "+dispatchDefaultTestVariant+"\nversion: 1.0.0\n"), common.TranslateBytesOptions{})
	if !assert.NoError(t, err) {
		return
	}
	want, err := os.Getwd()
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, want, sourceDir)
}
