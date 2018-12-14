/*
Copyright 2018 The Knative Authors.

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

package config

import (
	"errors"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	ConfigName = "config-tracing"

	enableKey                  = "enable"
	zipkinCollectorEndpointKey = "zipkinCollectorEndpoint"
	debugKey                   = "debug"
)

type Config struct {
	Enable                  bool
	ZipkinCollectorEndpoint string
	Debug                   bool
}

func (cfg *Config) Equals(other *Config) bool {
	return other.Enable == cfg.Enable && other.ZipkinCollectorEndpoint == cfg.ZipkinCollectorEndpoint && other.Debug == cfg.Debug
}

func NewTracingConfigFromMap(cfgMap map[string]string) (*Config, error) {
	tc := Config{}
	if enable, ok := cfgMap[enableKey]; !ok {
		tc.Enable = false
	} else {
		if enableBool, err := strconv.ParseBool(enable); err != nil {
			return nil, fmt.Errorf("Failed parsing tracing config %q", enableKey)
		} else {
			tc.Enable = enableBool
		}
	}

	if endpoint, ok := cfgMap[zipkinCollectorEndpointKey]; !ok {
		if tc.Enable {
			return nil, errors.New("Tracing enabled but no collector endpoint specified")
		}
	} else {
		tc.ZipkinCollectorEndpoint = endpoint
	}

	if debug, ok := cfgMap[debugKey]; !ok {
		tc.Debug = false
	} else {
		if debugBool, err := strconv.ParseBool(debug); err != nil {
			return nil, fmt.Errorf("Failed parsing tracing config %q", debugKey)
		} else {
			tc.Debug = debugBool
		}
	}

	return &tc, nil
}

func NewTracingConfigFromConfigMap(config *corev1.ConfigMap) (*Config, error) {
	return NewTracingConfigFromMap(config.Data)
}
