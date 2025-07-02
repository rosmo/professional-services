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
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	celtypes "github.com/google/cel-go/common/types"
	"github.com/oschwald/geoip2-golang"
	compute "google.golang.org/api/compute/v1"

	"github.com/rs/zerolog/log"
)

type RuleEvaluationFailed struct {
}

func (e *RuleEvaluationFailed) Error() string {
	return fmt.Sprintf("Rule evaluation failed")
}

func (er *EvaluationRequests) EvaluateRequests(policy *SecurityPolicy) error {
	requests := er

	for ridx, req := range requests.HttpRequests {
		log.Info().Int("request", ridx).Str("description", req.Description).Msg("Evaluating HTTP request against policy")
		for ruleIdx, rule := range policy.Rules {
			requests.CurrentRequest = &req
			requests.CurrentPolicy = policy

			if rule.Match.VersionedExpr == "SRC_IPS_V1" {
				return fmt.Errorf("Versioned expression of type SRC_IPS_V1 are not supported")
			}

			if rule.Preview && !req.EvaluatePreviewRules {
				log.Warn().Msgf("Skipping preview rule #%d...", ruleIdx+1)
				continue
			}

			log.Info().Msgf("Evaluating rule #%d (priority %d) with request #%d: %s", ruleIdx+1, rule.Priority, ridx+1, rule.Match.Expr.Expression)
			err := er.EvaluateSecurityRule(rule, req.HttpRequest, map[string]interface{}{
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

func (er *EvaluationRequests) EvaluateSecurityRule(rule *compute.SecurityPolicyRule, r *http.Request, attributes map[string]interface{}) error {
	// Set up CEL environment
	ctx := context.WithValue(context.Background(), "reqs", er)

	celEnv, err := GetCelEnv(ctx)
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

	if origin, ok := celParams["origin"].(map[string]interface{}); ok {
		if ip, ok := origin["ip"].(string); ok {
			if userIp, ok := origin["userIp"]; ok {
				ip = userIp.(string)
			} else {
				origin["user_ip"] = ip
			}

			asn := uint(0)
			country := "US"
			if er.GeoIP.Country != "" {
				db, err := geoip2.Open(er.GeoIP.Country)
				if err != nil {
					return err
				}
				defer db.Close()
				ipAddr := net.ParseIP(ip)
				record, err := db.Country(ipAddr)
				if err == nil && record.Country.IsoCode != "" {
					country = record.Country.IsoCode
				}
			}
			if er.GeoIP.ASN != "" {
				db, err := geoip2.Open(er.GeoIP.ASN)
				if err != nil {
					return err
				}
				defer db.Close()
				ipAddr := net.ParseIP(ip)
				record, err := db.ASN(ipAddr)
				if err == nil && record.AutonomousSystemNumber != 0 {
					asn = record.AutonomousSystemNumber
				}
			}
			origin["asn"] = asn
			origin["country"] = country
		} else {
			log.Warn().Msg("No user IP in origin, unable to determine GeoIP.")
		}
	} else {
		log.Warn().Msg("No user IP in origin, unable to determine GeoIP.")
	}

	out, _, err := celProgram.ContextEval(ctx, celParams)
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
