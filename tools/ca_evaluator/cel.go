package caevaluator

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
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/corazawaf/coraza/v3"
	corazatypes "github.com/corazawaf/coraza/v3/types"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/ext"
	"github.com/rs/zerolog/log"
)

type CelLib struct{}

func urlDecode(qstr ref.Val) ref.Val {
	_qstr, err := qstr.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return types.NewErr("URL is not a string")
	}

	decoded, err := url.QueryUnescape(_qstr.(string))
	if err != nil {
		return types.NewErr("URL cannot be decoded: %s", err.Error())
	}

	return types.String(decoded)
}

func utf8ToUnicode(s ref.Val) ref.Val {
	_s, err := s.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return types.NewErr("Not a string")
	}

	return types.String(strings.ToLower(_s.(string)))
}

func evaluatePreconfiguredWaf(ctx context.Context, ruleset ref.Val, rulecfg *ref.Val) ref.Val {
	evaluationRequests := ctx.Value("reqs").(*EvaluationRequests)

	_ruleset, err := ruleset.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return types.NewErr("Ruleset is not a string")
	}
	_rulecfg := make(map[string]interface{}, 0)
	if rulecfg != nil {
		rulecfgtype := make(map[string]interface{})
		__rulecfg, err := (*rulecfg).ConvertToNative(reflect.TypeOf(rulecfgtype))
		if err != nil {
			return types.NewErr("Rule config is not a map of string")
		}
		_rulecfg = __rulecfg.(map[string]interface{})
	}

	var directives string
	var ok bool
	if directives, ok = evaluationRequests.CrsRulesAll[_ruleset.(string)]; !ok {
		return types.NewErr(fmt.Sprintf("Unconfigured rule set specified: %s", _ruleset))
	}

	// directives = "SecDebugLog debug.log\nSecDebugLogLevel 9\n" + directives

	var jsonContentTypes = []string{
		"application/json",
		"application/vnd.api+json",
		"application/vnd.collection+json",
		"application/vnd.hyper+json",
	}
	if evaluationRequests.CurrentPolicy.AdvancedOptionsConfig != nil {
		if evaluationRequests.CurrentPolicy.AdvancedOptionsConfig.JsonParsing == "STANDARD" || evaluationRequests.CurrentPolicy.AdvancedOptionsConfig.JsonParsing == "STANDARD_WITH_GRAPHQL" {

			if evaluationRequests.CurrentPolicy.AdvancedOptionsConfig.JsonCustomConfig != nil {
				if len(evaluationRequests.CurrentPolicy.AdvancedOptionsConfig.JsonCustomConfig.ContentTypes) > 0 {
					jsonContentTypes = evaluationRequests.CurrentPolicy.AdvancedOptionsConfig.JsonCustomConfig.ContentTypes
				}
			}
			ct := fmt.Sprintf("(%s)", strings.Join(jsonContentTypes, "|"))
			directives = fmt.Sprintf("SecRule REQUEST_HEADERS:Content-Type \"^%s\" \"id:'200001',phase:1,t:none,t:lowercase,pass,nolog,ctl:requestBodyProcessor=JSON\"\n", ct) + "\n" + directives
			directives += "SecRule REQBODY_ERROR \"!@eq 0\" \"id:'200002', phase:2,t:none,log,deny,status:400,msg:'Failed to parse request body.',logdata:'%{reqbody_error_msg}',severity:2\"\n"
		}
	}

	var requestPassed bool = true
	var delayedError *ref.Val

	sensitivityLevel := 1
	if level, ok := _rulecfg["sensitivity"]; ok {
		sensitivityLevel = int(level.(int64))
	}
	optOutRules := make([]string, 0)
	if optOut, ok := _rulecfg["opt_out_rule_ids"]; ok {
		for _, oo := range optOut.([]ref.Val) {
			_oo, err := oo.ConvertToNative(reflect.TypeOf(""))
			if err != nil {
				return types.NewErr(fmt.Sprintf("Invalid opt out rule ID: %s", err))
			}
			optOutRules = append(optOutRules, _oo.(string))
		}
	}

	optInRules := make([]string, 0)
	if optIn, ok := _rulecfg["opt_in_rule_ids"]; ok {
		for _, oo := range optIn.([]ref.Val) {
			_oo, err := oo.ConvertToNative(reflect.TypeOf(""))
			if err != nil {
				return types.NewErr(fmt.Sprintf("Invalid opt in rule ID: %s", err))
			}
			optInRules = append(optInRules, _oo.(string))
		}
	}

	wafFs := os.DirFS(evaluationRequests.CrsRulesBasePath[_ruleset.(string)])
	wafConfig := coraza.NewWAFConfig().WithDirectives(directives).WithRootFS(wafFs).WithRequestBodyAccess().WithErrorCallback(func(rule corazatypes.MatchedRule) {
		idTpl := template.New("RuleID")
		idTpl, err := idTpl.Parse(evaluationRequests.CrsRules[_ruleset.(string)].ID)
		if err != nil {
			derr := types.NewErr(fmt.Sprintf("Invalid ID template for ruleset %s: %s", _ruleset, evaluationRequests.CrsRules[_ruleset.(string)].ID))
			delayedError = &derr
			return
		}
		var ruleIdBuf bytes.Buffer
		idTpl.Execute(&ruleIdBuf, map[string]interface{}{
			"Id":       rule.Rule().ID(),
			"ID":       rule.Rule().ID(),
			"Line":     rule.Rule().Line(),
			"Revision": rule.Rule().Revision(),
			"Version":  rule.Rule().Version(),
			"Maturity": rule.Rule().Revision(),
			"Accuracy": rule.Rule().Accuracy(),
			"SecMark":  rule.Rule().SecMark(),
		})
		ruleId := ruleIdBuf.String()

		paranoiaLevel := 1
		for _, tag := range rule.Rule().Tags() {
			if strings.HasPrefix(tag, "paranoia-level/") {
				t := strings.SplitN(tag, "/", 2)
				paranoiaLevel, err = strconv.Atoi(t[1])
				if err != nil {
					derr := types.NewErr(fmt.Sprintf("Invalid paranoia level in rule %s: %s", rule.Rule().ID(), tag))
					delayedError = &derr
					return
				}
			}
		}
		if paranoiaLevel <= sensitivityLevel && ((len(optInRules) > 0 && slices.Contains(optInRules, ruleId)) || !slices.Contains(optOutRules, ruleId)) {
			log.Info().Str("rule_id", ruleId).Msgf("WAF finding: %s", ruleId)
			requestPassed = false
		} else {
			if paranoiaLevel > sensitivityLevel {
				log.Info().Int("paranoia_level", paranoiaLevel).Int("sensitivty_level", sensitivityLevel).Msg("Ignoring WAF finding due to sensitivity level")
			} else {
				log.Info().Int("paranoia_level", paranoiaLevel).Int("sensitivty_level", sensitivityLevel).Msg("Ignoring WAF finding due to opt out or opt in")
			}
		}
	})
	waf, err := coraza.NewWAF(wafConfig)
	if err != nil {
		return types.NewErr(fmt.Sprintf("Unable to initialize WAF: %s", err))
	}
	tx := waf.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		tx.Close()
	}()

	ipAddr, ok := evaluationRequests.CurrentRequest.Origin["ip"]
	if !ok {
		ipAddr = "127.0.0.1"
	}

	httpReq := evaluationRequests.CurrentRequest.HttpRequest
	tx.ProcessConnection(ipAddr.(string), 443, "127.0.0.1", 443)
	tx.ProcessURI(httpReq.URL.String(), httpReq.Method, httpReq.Proto)
	for k, vals := range httpReq.Header {
		for _, val := range vals {
			tx.AddRequestHeader(k, val)
		}
	}
	if httpReq.Host != "" {
		tx.AddRequestHeader("Host", httpReq.Host)
		tx.SetServerName(httpReq.Host)
	}
	if httpReq.TransferEncoding != nil {
		tx.AddRequestHeader("Transfer-Encoding", httpReq.TransferEncoding[0])
	}
	tx.ProcessRequestHeaders()
	if tx.IsRequestBodyAccessible() {
		if httpReq.Body != nil && httpReq.Body != http.NoBody {
			tx.WriteRequestBody(evaluationRequests.CurrentRequest.RequestBody)
		}
	}
	_, err = tx.ProcessRequestBody()
	if err != nil {
		return types.NewErr(fmt.Sprintf("Failed to process request body: %s", err.Error()))
	}
	if delayedError != nil {
		return *delayedError
	}

	// Reverse the logic, if request passed, if should return false
	return types.Bool(!requestPassed)
}

func ipInRange(ipAddr, ipRange ref.Val) ref.Val {
	_ipAddr, err := ipAddr.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return types.NewErr("IP address is not a string")
	}
	_ipRange, err := ipRange.ConvertToNative(reflect.TypeOf(""))
	if err != nil {
		return types.NewErr("IP range is not a string")
	}

	ipAddrParsed := net.ParseIP(_ipAddr.(string))
	if ipAddrParsed == nil {
		return types.NewErr("Invalid IP address")
	}

	_, ipRangeParsed, err := net.ParseCIDR(_ipRange.(string))
	if err != nil {
		return types.NewErr("Invalid IP range")
	}

	if ipRangeParsed.Contains(ipAddrParsed) {
		return types.Bool(true)
	}
	return types.Bool(false)
}

func evaluateAdaptiveProtection(id ref.Val) ref.Val {
	return types.Bool(false)
}

func evaluateAdaptiveProtectionAutoDeploy(values ...ref.Val) ref.Val {
	return types.Bool(false)
}

func getCelFunctions(ctx context.Context) []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function("evaluatePreconfiguredWaf",
			cel.Overload("bool_evaluatePreconfiguredWaf_string_map",
				[]*cel.Type{cel.StringType, cel.MapType(cel.StringType, cel.DynType)},
				cel.BoolType,
				cel.BinaryBinding(
					func(ruleset ref.Val, rulecfg ref.Val) ref.Val {
						return evaluatePreconfiguredWaf(ctx, ruleset, &rulecfg)
					},
				),
			),
			cel.Overload("bool_evaluatePredefinedWaf_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(
					func(ruleset ref.Val) ref.Val {
						return evaluatePreconfiguredWaf(ctx, ruleset, nil)
					},
				),
			),
		),
		cel.Function("evaluateAdaptiveProtection",
			cel.Overload("string_evaluateAdaptiveProtection_string",
				[]*cel.Type{cel.StringType},
				cel.BoolType,
				cel.UnaryBinding(evaluateAdaptiveProtection),
			),
		),
		cel.Function("evaluateAdaptiveProtectionAutoDeploy",
			cel.Overload("evaluateAdaptiveProtectionAutoDeploy",
				[]*cel.Type{},
				cel.BoolType,
				cel.FunctionBinding(evaluateAdaptiveProtectionAutoDeploy),
			),
		),
		cel.Function("ipInRange",
			cel.Overload("string_ipInRange_string",
				[]*cel.Type{cel.StringType, cel.StringType},
				cel.BoolType,
				cel.BinaryBinding(ipInRange),
			),
		),
		cel.Function("urlDecode",
			cel.MemberOverload("string_urldecode_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(urlDecode),
			),
		),
		cel.Function("urlDecodeUni",
			cel.MemberOverload("string_urldecodeuni_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(urlDecode),
			),
		),
		cel.Function("utf8ToUnicode",
			cel.MemberOverload("string_utf8tounicode_string",
				[]*cel.Type{cel.StringType},
				cel.StringType,
				cel.UnaryBinding(utf8ToUnicode),
			),
		),
	}
}

func GetCelEnv(ctx context.Context) (*cel.Env, error) {
	env, err := cel.NewEnv(
		ext.Strings(),
		ext.Encoders(),
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("origin", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, err
	}
	for _, f := range getCelFunctions(ctx) {
		env, err = env.Extend(f)
		if err != nil {
			return nil, err
		}
	}
	return env, err
}

func GetCelProgram(env *cel.Env, expr string, mustBeBool bool) (cel.Program, error) {
	celAst, celIss := env.Compile(expr)
	if celIss.Err() != nil {
		return nil, fmt.Errorf("Encountered error when compiling instance CEL: %s\n", celIss.Err())
	}
	if mustBeBool {
		if celAst.OutputType() != cel.BoolType {
			return nil, fmt.Errorf("Error compiling CEL, got %v as return value, wanted type bool", celAst.OutputType())
		}
	}

	celPrg, err := env.Program(celAst)
	if err != nil {
		return nil, fmt.Errorf("Encountered error when processing instance CEL: %s\n", err.Error())
	}
	return celPrg, nil
}
