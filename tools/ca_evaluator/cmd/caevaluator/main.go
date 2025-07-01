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
	"flag"
	"fmt"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	caevaluator "github.com/GoogleCloudPlatform/professional-services/tools/ca_evaluator"
)

func main() {
	requestsFilePtr := flag.String("http-requests", "", "HTTP requests in a YAML file to be evaluated")
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	jsonPtr := flag.Bool("json", false, "Output results as JSON")

	flag.Parse()

	if *requestsFilePtr == "" {
		log.Fatal().Msg("No YAML file containing requests to be evaluated was found, specify it via -http-requests.")
	}

	rf, err := os.Open(*requestsFilePtr)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to open evaluation requests file: %s", *requestsFilePtr)
	}
	defer rf.Close()
	log.Info().Msgf("Reading HTTP requests: %s", *requestsFilePtr)
	frf := bufio.NewReader(rf)
	evaluationRequests, err := caevaluator.ParseEvaluationRequests(frf)
	if err != nil {
		log.Fatal().Err(err).Msgf("Unable to parse evaluation requests file: %s", *requestsFilePtr)
	}

	for _, policyFile := range flag.Args() {
		log.Info().Msgf("Reading policy: %s", policyFile)

		f, err := os.Open(policyFile)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to open policy file: %s", policyFile)
		}
		defer f.Close()

		fr := bufio.NewReader(f)
		policy, err := caevaluator.ParseSecurityPolicy(fr)
		if err != nil {
			log.Fatal().Err(err).Msgf("Unable to parse policy file: %s", policyFile)
		}

		err = caevaluator.EvaluateRequests(policy, evaluationRequests)
		if err != nil {
			if *jsonPtr == true {
				fmt.Printf("{\"success\":false}")
				os.Exit(1)
			} else {
				log.Fatal().Err(err).Msg("Policy evaluation failed.")
			}
		}
	}
	if *jsonPtr == true {
		fmt.Printf("{\"success\":true}")
	}
	os.Exit(0)
}
