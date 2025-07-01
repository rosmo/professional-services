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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	celtypes "github.com/google/cel-go/common/types"
	compute "google.golang.org/api/compute/v1"

	"github.com/rs/zerolog/log"
)

type RuleEvaluationFailed struct {
}

func (e *RuleEvaluationFailed) Error() string {
	return fmt.Sprintf("Rule evaluation failed")
}

func EvaluateRequests(policy *compute.SecurityPolicy, requests *EvaluationRequests) error {
	CurrentEvaluationRequests = requests

	for ridx, req := range requests.HttpRequests {
		log.Info().Int("request", ridx).Str("description", req.Description).Msg("Evaluating HTTP request against policy")
		for ruleIdx, rule := range policy.Rules {
			CurrentEvaluationRequests.CurrentRequest = &req
			CurrentEvaluationRequests.CurrentPolicy = policy

			if rule.Match.VersionedExpr == "SRC_IPS_V1" {
				return fmt.Errorf("Versioned expression of type SRC_IPS_V1 are not supported")
			}

			if rule.Preview && !req.EvaluatePreviewRules {
				log.Warn().Msgf("Skipping preview rule #%d...", ruleIdx+1)
				continue
			}

			log.Info().Msgf("Evaluating rule #%d (priority %d) with request #%d: %s", ruleIdx+1, rule.Priority, ridx+1, rule.Match.Expr.Expression)
			err := EvaluateSecurityRule(rule, req.HttpRequest, map[string]interface{}{
				"origin": req.Origin,
			})

			if err == nil {
				if rule.Action != req.Expect {
					log.Error().Str("action", rule.Action).Str("expected", req.Expect).Msgf("Rule evaluation failed: unexpected action.")
					return fmt.Errorf("Rule evaluation failed: unexpected action.")
				} else {
					log.Info().Str("action", rule.Action).Str("expected", req.Expect).Msgf("Policy evaluated: expected action.")
					break
				}
			}

			failed := &RuleEvaluationFailed{}
			if err != nil && !errors.As(err, &failed) {
				return err
			}
		}
	}
	return nil
}

func EvaluateSecurityRule(rule *compute.SecurityPolicyRule, r *http.Request, attributes map[string]interface{}) error {
	// Set up CEL environment
	celEnv, err := GetCelEnv()
	if err != nil {
		return err
	}
	expression := rule.Match.Expr.Expression
	celProgram, err := GetCelProgram(celEnv, expression, true)
	if err != nil {
		return err
	}

	currentTime := time.Now()
	currentTimeUTC := currentTime.UTC()

	httpHeaders := make(map[string]string, 0)
	for k, values := range r.Header {
		if len(values) == 1 {
			httpHeaders[strings.ToLower(k)] = values[0]
		} else {
			httpHeaders[strings.ToLower(k)] = strings.Join(values, ",")
		}
	}
	celParams := map[string]interface{}{
		"request": map[string]interface{}{
			"method":   r.Method,
			"path":     r.URL.RawPath,
			"scheme":   r.URL.Scheme,
			"query":    r.URL.RawQuery,
			"headers":  httpHeaders,
			"unixtime": currentTime.Unix(),
			"time": map[string]int{
				"year":   currentTimeUTC.Year(),
				"month":  int(currentTimeUTC.Month()),
				"day":    currentTimeUTC.Day(),
				"hour":   currentTimeUTC.Hour(),
				"minute": currentTimeUTC.Minute(),
				"second": currentTimeUTC.Second(),
			},
		},
	}
	for k, v := range attributes {
		celParams[k] = v
	}

	out, _, err := celProgram.Eval(celParams)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to evaluate Cloud Armor expression: %v", out)
		return nil
	}

	result, ok := out.(celtypes.Bool)
	if !ok || result.Equal(celtypes.False).Value().(bool) {
		return &RuleEvaluationFailed{}
	}

	return nil
}
