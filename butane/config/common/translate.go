// Copyright 2019 Red Hat, Inc
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

package common

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/vcontext/report"
	"gopkg.in/yaml.v3"
)

var registry = map[string]Translator{}

// Fields that must be included in the root struct of every spec version.
type commonFields struct {
	Version string `yaml:"version"`
	Variant string `yaml:"variant"`
}

// Translator translates a raw Butane config to a raw Ignition config.
// The report should include errors, warnings, and other messages from
// translation. Fatal reports or other translation failures should return an
// error.
type Translator func([]byte, TranslateBytesOptions) ([]byte, report.Report, error)

// RegisterTranslator registers a translator for the specified variant and
// version to be available for use by TranslateBytes. This is only needed by
// users implementing translators outside the Butane package.
func RegisterTranslator(variant, version string, trans Translator) {
	key := fmt.Sprintf("%s+%s", variant, version)
	if _, ok := registry[key]; ok {
		panic("tried to reregister existing translator")
	}
	registry[key] = trans
}

func getTranslator(variant string, version semver.Version) (Translator, error) {
	t, ok := registry[fmt.Sprintf("%s+%s", variant, version.String())]
	if !ok {
		return nil, ErrUnknownVersion{
			Variant: variant,
			Version: version,
		}
	}
	return t, nil
}

// TranslateBytes dispatches to the translator for the input's variant and
// version. When called recursively with options inherited from a parent
// translation, the child must use the same variant and version.
func TranslateBytes(input []byte, options TranslateBytesOptions) ([]byte, report.Report, error) {
	ver := commonFields{}
	if err := yaml.Unmarshal(input, &ver); err != nil {
		return nil, report.Report{}, ErrUnmarshal{Detail: err.Error()}
	}
	if ver.Variant == "" {
		return nil, report.Report{}, ErrNoVariant
	}

	tmp, err := semver.NewVersion(ver.Version)
	if err != nil {
		return nil, report.Report{}, ErrInvalidVersion
	}
	version := *tmp

	canonicalVersion := version
	canonicalVersion.Metadata = ""
	if options.parentVariant != "" && (ver.Variant != options.parentVariant || canonicalVersion.String() != options.parentVersion) {
		return nil, report.Report{}, fmt.Errorf(
			"imported Butane config must use variant %q and version %q; got variant %q and version %q",
			options.parentVariant,
			options.parentVersion,
			ver.Variant,
			canonicalVersion.String(),
		)
	}

	translator, err := getTranslator(ver.Variant, version)
	if err != nil {
		return nil, report.Report{}, err
	}

	if options.SourceDir == "" {
		options.SourceDir, err = os.Getwd()
	} else {
		options.SourceDir, err = filepath.Abs(options.SourceDir)
	}
	if err != nil {
		return nil, report.Report{}, fmt.Errorf("resolving Butane source directory: %w", err)
	}

	if options.parentVariant == "" {
		options.parentVariant = ver.Variant
		options.parentVersion = canonicalVersion.String()
	}
	return translator(input, options)
}
