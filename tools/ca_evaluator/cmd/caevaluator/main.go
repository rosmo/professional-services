package main

//    Copyright 2023 Google LLC
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
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/ettle/strcase"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	caevaluator "github.com/GoogleCloudPlatform/professional-services/tools/ca_evaluator"
)

type StdinInput struct {
	Requests string `yaml:"requests,omitempty" json:"requests,omitempty"`
	Policy   string `yaml:"security_policy,omitempty" json:"security_policy,omitempty"`
}

type TerraformPolicy struct {
}

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

func convertTerraform(old interface{}) interface{} {
	switch reflect.TypeOf(old).Kind() {
	case reflect.Map:
		n := make(map[string]interface{}, 0)
		for k, v := range old.(map[string]interface{}) {
			switch k {
			case "rule":
				k = "rules"
			default:
				k = strcase.ToCamel(k)
			}
			converted := convertTerraform(v)

			// Unwind arrays
			switch k {
			case "advancedOptionsConfig", "adaptiveProtectionConfig", "layer7DdosDefenseConfig", "jsonCustomConfig", "ddosProtectionConfig", "recaptchaOptionsConfig", "headerAction", "match", "config", "rateLimitOptions", "redirectOptions", "expr", "preconfiguredWafConfig":
				if len(converted.([]interface{})) > 0 {
					n[k] = converted.([]interface{})[0]
				}
			default:
				n[k] = converted
			}
		}
		return n
	case reflect.Array:
	case reflect.Slice:
		n := make([]interface{}, 0)
		for _, v := range old.([]interface{}) {
			n = append(n, convertTerraform(v))
		}
		return n
	}
	return old
}

func main() {
	policyFilePtr := flag.String("security-policy", "", "Cloud Armor policy in a JSON file")
	requestsFilePtr := flag.String("http-requests", "", "HTTP requests in a YAML file to be evaluated")
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	jsonOutputPtr := flag.Bool("json-output", false, "Output results as JSON")
	jsonInputPtr := flag.Bool("json-input", false, "Output results as JSON")
	convertTerraformPtr := flag.Bool("convert-terraform", false, "Convert Terraform resource output")
	flag.Parse()

	if !*jsonInputPtr {
		if *requestsFilePtr == "" {
			log.Fatal().Msg("No YAML file containing requests to be evaluated was found, specify it via -http-requests.")
		}
		if *policyFilePtr == "" {
			log.Fatal().Msg("No JSON file containing Cloud Armor policy was found, specify it via -security-policy.")
		}
	}

	if *jsonInputPtr {
		var r = bufio.NewReader(os.Stdin)

		var contents []byte
		var err error
		contents, err = io.ReadAll(r)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to read input from stdin!")
		}

		input := StdinInput{}
		if err = json.Unmarshal(contents, &input); err != nil {
			log.Fatal().Err(err).Msg("Malformed input on stdin!")
		}

		if *convertTerraformPtr {
			// Disable all logging for Terraform
			zerolog.SetGlobalLevel(zerolog.FatalLevel)

			tfPolicyOld := make(map[string]interface{}, 0)
			if err = json.Unmarshal([]byte(input.Policy), &tfPolicyOld); err != nil {
				log.Fatal().Err(err).Msg("Malformed policy on stdin!")
			}

			tfPolicyNew := convertTerraform(tfPolicyOld)
			var tmpPolicy []byte
			tmpPolicy, err = json.Marshal(tfPolicyNew)
			if err != nil {
				log.Fatal().Err(err).Msg("Could not convert policy from Terraform")
			}
			input.Policy = string(tmpPolicy)
		}

		policy := caevaluator.NewSecurityPolicy()
		err = policy.ParseSecurityPolicy(strings.NewReader(input.Policy))
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to parse policy")
		}

		evaluationRequests := caevaluator.NewEvaluationRequests()
		err = evaluationRequests.ParseEvaluationRequests(strings.NewReader(input.Requests))
		err = evaluationRequests.EvaluateRequests(policy)
		if err != nil {
			if *jsonOutputPtr == true {
				fmt.Printf("{\"success\":\"false\"}")
				if !*convertTerraformPtr {
					os.Exit(1)
				} else {
					os.Exit(0)
				}
			} else {
				log.Fatal().Err(err).Msg("Policy evaluation failed.")
			}
		}
		if *jsonOutputPtr == true {
			fmt.Printf("{\"success\":\"true\"}")
		}

	} else {
		rf, err := os.Open(*requestsFilePtr)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to open evaluation requests file: %s", *requestsFilePtr)
		}
		defer rf.Close()
		log.Info().Msgf("Reading HTTP requests: %s", *requestsFilePtr)
		frf := bufio.NewReader(rf)
		evaluationRequests := caevaluator.NewEvaluationRequests()
		err = evaluationRequests.ParseEvaluationRequests(frf)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to parse evaluation requests file: %s", *requestsFilePtr)
		}

		log.Info().Msgf("Reading policy: %s", *policyFilePtr)

		f, err := os.Open(*policyFilePtr)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to open policy file: %s", *policyFilePtr)
		}
		defer f.Close()

		fr := bufio.NewReader(f)
		policy := caevaluator.NewSecurityPolicy()
		err = policy.ParseSecurityPolicy(fr)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to parse policy file: %s", *policyFilePtr)
		}

		err = evaluationRequests.EvaluateRequests(policy)
		if err != nil {
			if *jsonOutputPtr == true {
				fmt.Printf("{\"success\":\"false\"}")
				os.Exit(1)
			} else {
				log.Fatal().Err(err).Msg("Policy evaluation failed.")
			}
		}
		if *jsonOutputPtr == true {
			fmt.Printf("{\"success\":\"true\"}")
		}
	}
	os.Exit(0)
}
