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
	"encoding/json"
	"io"
	"slices"

	compute "google.golang.org/api/compute/v1"
)

type SecurityPolicy struct {
	compute.SecurityPolicy
	// Id causes issues as it's defined as uint64 which unmarshaler doesn't like
	Id string `json:"id,omitempty"`
}

func NewSecurityPolicy() *SecurityPolicy {
	return &SecurityPolicy{}
}

func (sp *SecurityPolicy) ParseSecurityPolicy(contents io.Reader) error {
	policyContents, err := io.ReadAll(contents)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(policyContents, sp); err != nil {
		return err
	}

	// Sort rules by priority
	slices.SortFunc(sp.Rules, func(a, b *compute.SecurityPolicyRule) int {
		if a.Priority > b.Priority {
			return 1
		} else if a.Priority < b.Priority {
			return -1
		}
		return 0
	})

	// Print out the new struct
	return nil
}
