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
	"strings"

	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

type EvaluationRequest struct {
	Origin               map[string]interface{} `yaml:"origin,omitempty" json:"origin,omitempty"`
	Description          string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Request              string                 `yaml:"request,omitempty" json:"request,omitempty"`
	RequestType          string                 `yaml:"requestType,omitempty" json:"requestType,omitempty"`
	HttpRequest          *http.Request          `yaml:"-" json:"-"`
	RequestBody          []byte                 `yaml:"-" json:"-"`
	Expect               string                 `yaml:"expect,omitempty" json:"expect,omitempty"`
	EvaluatePreviewRules bool                   `yaml:"evaluatePreviewRules,omitempty" json:"evaluatePreviewRules,omitempty"`
}

type CrsRule struct {
	ID      string   `yaml:"id,omitempty" json:"id,omitempty"`
	Config  string   `yaml:"config,omitempty" json:"config,omitempty"`
	Sources []string `yaml:"sources,omitempty" json:"sources,omitempty"`
}

type GeoIPDatabase struct {
	Country string `yaml:"country,omitempty" json:"country,omitempty"`
	ASN     string `yaml:"asn,omitempty" json:"asn,omitempty"`
}

type EvaluationRequests struct {
	HttpRequests []EvaluationRequest `yaml:"httpRequests,omitempty" json:"httpRequests,omitempty"`
	GeoIP        GeoIPDatabase       `yaml:"geoIp,omitempty" json:"geoIp,omitempty"`
	CrsRules     map[string]CrsRule  `yaml:"crsRules,omitempty" json:"crsRules,omitempty"`

	CrsRulesAll      map[string]string `yaml:"-" json:"-"`
	CrsRulesBasePath map[string]string `yaml:"-" json:"-"`

	CurrentRequest *EvaluationRequest `yaml:"-" json:"-"`
	CurrentPolicy  *SecurityPolicy    `yaml:"-" json:"-"`
}

func NewEvaluationRequests() *EvaluationRequests {
	return &EvaluationRequests{}
}

func (er *EvaluationRequests) ParseEvaluationRequests(contents io.Reader) error {
	requestsContents, err := io.ReadAll(contents)
	if err != nil {
		return err
	}

	if yamlErr := yaml.Unmarshal(requestsContents, er); err != nil {
		if jsonErr := json.Unmarshal(requestsContents, er); err != nil {
			return fmt.Errorf("Unable to parse requests as YAML or JSON: %s, %s", yamlErr.Error(), jsonErr.Error())
		}
	}

	if len(er.CrsRules) == 0 {
		log.Warn().Msg("No Core Rule Set rule configurations specified, pre-defined WAF rules will not be evaluated")
	}

	er.CrsRulesAll = make(map[string]string, 0)
	er.CrsRulesBasePath = make(map[string]string, 0)
	for id, crsrule := range er.CrsRules {
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
					er.CrsRulesBasePath[id] = filepath.Dir(file)
				}
			}
		}
		allRules += "\n" + crsrule.Config + "\n"
		er.CrsRulesAll[id] = string(allRules)
	}

	for idx, req := range er.HttpRequests {
		if req.RequestType == "HTTP2" {
			return fmt.Errorf("HTTP2 requests are not yet supported")
		} else {
			er.HttpRequests[idx].HttpRequest, err = http.ReadRequest(bufio.NewReader(strings.NewReader(req.Request)))
		}
		if err == io.EOF {
			break
		} else {
			body, err := io.ReadAll(er.HttpRequests[idx].HttpRequest.Body)
			if err != nil {
				return fmt.Errorf("Failed to read request body: %s: %s\n", err.Error(), req.Request)
			}
			er.HttpRequests[idx].RequestBody = body
		}
		if err != nil {
			return fmt.Errorf("Failed to parse request: %s: %s\n", err.Error(), req.Request)
		}
	}
	return nil
}
