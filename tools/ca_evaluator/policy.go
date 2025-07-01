package caevaluator

//    Copyright 2025 Google LLC
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	compute "google.golang.org/api/compute/v1"
	"gopkg.in/yaml.v3"
)

type SecurityPolicy struct {
	compute.SecurityPolicy
	// Id causes issues as it's defined as uint64 which unmarshaler doesn't like
	Id string `json:"id,omitempty,string" yaml:"id,omitempty,string"`
}

type EvaluationRequest struct {
	Origin               map[string]interface{} `yaml:"origin,omitempty"`
	Description          string                 `yaml:"description,omitempty"`
	Request              string                 `yaml:"request,omitempty"`
	RequestType          string                 `yaml:"requestType,omitempty"`
	HttpRequest          *http.Request
	RequestBody          []byte
	Expect               string `yaml:"expect,omitempty"`
	EvaluatePreviewRules bool   `yaml:"evaluatePreviewRules,omitempty"`
}

type CrsRule struct {
	ID      string   `yaml:"id,omitempty"`
	Config  string   `yaml:"config,omitempty"`
	Sources []string `yaml:"sources,omitempty"`
}

type EvaluationRequests struct {
	HttpRequests     []EvaluationRequest `yaml:"httpRequests,omitempty"`
	CrsRules         map[string]CrsRule  `yaml:"crsRules,omitempty"`
	CrsRulesAll      map[string]string
	CrsRulesBasePath map[string]string

	CurrentRequest *EvaluationRequest
	CurrentPolicy  *compute.SecurityPolicy
}

func ParseEvaluationRequests(contents io.Reader) (*EvaluationRequests, error) {
	requestsContents, err := io.ReadAll(contents)
	if err != nil {
		return nil, err
	}

	var requests EvaluationRequests
	if err := yaml.Unmarshal(requestsContents, &requests); err != nil {
		return nil, err
	}

	if len(requests.CrsRules) == 0 {
		log.Warn().Msg("No Core Rule Set rule configurations specified, pre-defined WAF rules will not be evaluated")
	}

	requests.CrsRulesAll = make(map[string]string, 0)
	requests.CrsRulesBasePath = make(map[string]string, 0)
	for id, crsrule := range requests.CrsRules {
		var allRules string = ""

		for _, pattern := range crsrule.Sources {
			files, err := filepath.Glob(pattern)
			if err != nil {
				log.Error().Err(err).Msgf("Error looking for CRS configuration files: %s", pattern)
			} else {
				for _, file := range files {
					fc, err := os.ReadFile(file)
					if err != nil {
						log.Fatal().Err(err).Msgf("Error reading CRS configuration file: %s", file)
					}
					allRules += "\n" + string(fc)
					requests.CrsRulesBasePath[id] = filepath.Dir(file)
				}
			}
		}
		allRules += "\n" + crsrule.Config + "\n"
		requests.CrsRulesAll[id] = string(allRules)
	}

	for idx, req := range requests.HttpRequests {
		if req.RequestType == "HTTP2" {
			return nil, fmt.Errorf("HTTP2 requests are not yet supported")
		} else {
			requests.HttpRequests[idx].HttpRequest, err = http.ReadRequest(bufio.NewReader(strings.NewReader(req.Request)))
		}
		if err == io.EOF {
			break
		} else {
			body, err := io.ReadAll(requests.HttpRequests[idx].HttpRequest.Body)
			if err != nil {
				return nil, fmt.Errorf("Failed to read request body: %s: %s\n", err.Error(), req.Request)
			}
			requests.HttpRequests[idx].RequestBody = body
		}
		if err != nil {
			return nil, fmt.Errorf("Failed to parse request: %s: %s\n", err.Error(), req.Request)
		}
	}
	return &requests, nil
}

func ParseSecurityPolicy(contents io.Reader) (*compute.SecurityPolicy, error) {
	policyContents, err := io.ReadAll(contents)
	if err != nil {
		return nil, err
	}

	var policy compute.SecurityPolicy
	if err := json.Unmarshal(policyContents, &policy); err != nil {
		return nil, err
	}

	// Sort rules by priority
	slices.SortFunc(policy.Rules, func(a, b *compute.SecurityPolicyRule) int {
		if a.Priority > b.Priority {
			return 1
		} else if a.Priority < b.Priority {
			return -1
		}
		return 0
	})

	// Print out the new struct
	return &policy, nil
}
