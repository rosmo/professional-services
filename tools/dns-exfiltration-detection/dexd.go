/*
#   Copyright 2024 Google LLC
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
#
*/
package dexd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	logging "cloud.google.com/go/logging/apiv2"
	"cloud.google.com/go/logging/apiv2/loggingpb"
	"github.com/gobwas/glob"

	gomplate "github.com/hairyhenderson/gomplate/v4"
	oauth "golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/iterator"

	securitycenter "cloud.google.com/go/securitycenter/apiv2"
	"cloud.google.com/go/securitycenter/apiv2/securitycenterpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type LogConfig struct {
	ResourceNames []string `yaml:"resourceNames,omitempty"`
	Filter        string   `yaml:"filter"`
	PageSize      *int     `yaml:"pageSize,omitempty"`
}

type SccDetectionConfig struct {
	CreateFinding *bool                  `yaml:"createFinding,omitempty"`
	Finding       map[string]interface{} `yaml:"finding"`
}

type DetectionConfig struct {
	Action                string              `yaml:"action"`
	BitsOfEntropy         float64             `yaml:"bitsOfEntropy"`
	Ngrams                *int                `yaml:"ngrams,omitempty"`
	LogEntriesRequired    int                 `yaml:"logEntriesRequired"`
	MinimumLength         int                 `yaml:"minimumLength"`
	QueryParts            int                 `yaml:"queryParts"`
	AllowAllCharacters    *bool               `yaml:"allowAllCharacters"`
	SecurityCommandCenter *SccDetectionConfig `yaml:"securityCommandCenter"`
}

type SccConfig struct {
	Source string `yaml:"source"`
}

type AllowListConfig struct {
	Domains           []string `yaml:"domains,omitempty"`
	FromSecretManager string   `yaml:"fromSecretManager,omitempty"`
}

type BlockConfig struct {
	ResponsePolicy string `yaml:"responsePolicy,omitempty"`
	ProjectID      string `yaml:"projectId,omitempty"`
	RulePrefix     string `yaml:"rulePrefix,omitempty"`
	Destination    string `yaml:"destination,omitempty"`
}

type Config struct {
	LogLevel              string            `yaml:"logLevel,omitempty"`
	Logging               LogConfig         `yaml:"logging"`
	Detection             []DetectionConfig `yaml:"detection"`
	AllowList             AllowListConfig   `yaml:"allowList,omitempty"`
	Block                 BlockConfig       `yaml:"block,omitempty"`
	SecurityCommandCenter *SccConfig        `yaml:"securityCommandCenter,omitempty"`
}

type LogError struct {
	Message  string
	LogEntry map[string]interface{}
}

type Detection struct {
	Entropy            float64
	Domain             string
	NumberOfLogEntries int
	Queries            []map[string]interface{}
}

func (e *LogError) Error() string {
	logStr, _ := json.Marshal(e.LogEntry)
	return fmt.Sprintf("%s: %s", e.Message, logStr)
}

var templateFirstLog map[string]interface{}
var templateLogEntries []map[string]interface{}
var templateInsertIds string
var templateDetection Detection

func getFirstLog() map[string]interface{} {
	return templateFirstLog
}

func getLogEntries() []map[string]interface{} {
	return templateLogEntries
}

func getInsertIds() string {
	return templateInsertIds
}

func getDetection() Detection {
	return templateDetection
}

func renderSccFindingField(ctx *context.Context, v interface{}) (interface{}, error) {
	var result interface{}
	switch v.(type) {
	case string:
		var findingBuf bytes.Buffer
		tr := gomplate.NewRenderer(gomplate.RenderOptions{
			Funcs: map[string]any{
				"firstLog":   getFirstLog,
				"logEntries": getLogEntries,
				"insertIds":  getInsertIds,
				"detection":  getDetection,
			},
		})
		err := tr.Render(*ctx, "finding", v.(string), &findingBuf)
		if err != nil {
			return nil, err
		}
		result = findingBuf.String()
	case map[string]interface{}:
		result = make(map[string]interface{}, 0)
		for kk, vv := range v.(map[string]interface{}) {
			vvv, err := renderSccFindingField(ctx, vv)
			if err != nil {
				return nil, err
			}
			result.(map[string]interface{})[kk] = vvv
		}
	case []interface{}:
		result = make([]interface{}, 0)
		for _, vv := range v.([]interface{}) {
			vvv, err := renderSccFindingField(ctx, vv)
			if err != nil {
				return nil, err
			}
			result = append(result.([]interface{}), vvv)
		}
	}
	return result, nil
}

func CreateSccFinding(config Config, detectionConfig DetectionConfig, detection Detection) error {
	ctx := context.Background()

	templateFirstLog = detection.Queries[0]
	templateLogEntries = detection.Queries
	templateDetection = detection

	insertIds := make([]string, 0)
	var queries = 0
	slices.Reverse(detection.Queries)
	for _, query := range detection.Queries {
		insertIds = append(insertIds, fmt.Sprintf("\"%s\"", query["insert_id"].(string)))
		queries += 1
		if queries > 20 {
			break
		}
	}
	templateInsertIds = strings.Join(insertIds, " OR ")

	sccFinding := make(map[string]interface{}, 0)
	for k, v := range detectionConfig.SecurityCommandCenter.Finding {
		vv, err := renderSccFindingField(&ctx, v)
		if err != nil {
			return fmt.Errorf("Failed to render SCC finding template: %w", err)
		}
		sccFinding[k] = vv
	}

	eventTimeGo, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", sccFinding["eventTime"].(string))
	if err != nil {
		return fmt.Errorf("Failed to parse event time for SCC finding: %w", err)
	}
	eventTime := timestamppb.New(eventTimeGo)

	var findingState securitycenterpb.Finding_State = securitycenterpb.Finding_ACTIVE
	if strings.ToUpper(sccFinding["state"].(string)) == "INACTIVE" {
		findingState = securitycenterpb.Finding_INACTIVE
	}

	var sourceProperties map[string]*structpb.Value
	if sourceProps, ok := sccFinding["sourceProperties"].(map[string]string); ok {
		for k, v := range sourceProps {
			sourceProperties[k] = &structpb.Value{
				Kind: &structpb.Value_StringValue{StringValue: v},
			}
		}
	}

	var findingSeverity securitycenterpb.Finding_Severity = securitycenterpb.Finding_HIGH
	if severity, ok := sccFinding["severity"].(string); ok {
		switch strings.ToUpper(severity) {
		case "CRITICAL":
			findingSeverity = securitycenterpb.Finding_CRITICAL
		case "HIGH":
			findingSeverity = securitycenterpb.Finding_HIGH
		case "MEDIUM":
			findingSeverity = securitycenterpb.Finding_MEDIUM
		case "LOW":
			findingSeverity = securitycenterpb.Finding_LOW
		}
	}

	var findingExternalUri string = ""
	if externalUri, ok := sccFinding["externalUri"].(string); ok {
		findingExternalUri = externalUri
	}

	var findingDescription string = "Detected by dns-extiltration-detector"
	if description, ok := sccFinding["description"].(string); ok {
		findingDescription = description
	}

	var findingClass securitycenterpb.Finding_FindingClass = securitycenterpb.Finding_THREAT
	if class, ok := sccFinding["findingClass"].(string); ok {
		switch strings.ToUpper(class) {
		case "CLASS_UNSPECIFIED":
			findingClass = securitycenterpb.Finding_FINDING_CLASS_UNSPECIFIED
		case "THREAT":
			findingClass = securitycenterpb.Finding_THREAT
		case "VULNERABILITY":
			findingClass = securitycenterpb.Finding_VULNERABILITY
		case "MISCONFIGURATION":
			findingClass = securitycenterpb.Finding_MISCONFIGURATION
		case "OBSERVATION":
			findingClass = securitycenterpb.Finding_OBSERVATION
		case "SCC_ERROR":
			findingClass = securitycenterpb.Finding_SCC_ERROR
		case "POSTURE_VIOLATION":
			findingClass = securitycenterpb.Finding_POSTURE_VIOLATION
		case "TOXIC_COMBINATION":
			findingClass = securitycenterpb.Finding_TOXIC_COMBINATION
		}
	}

	c, err := securitycenter.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("securitycenter.NewClient creation failed: %w", err)
	}
	defer c.Close()

	finding := &securitycenterpb.Finding{
		State:            findingState,
		ResourceName:     strings.TrimSpace(sccFinding["resourceName"].(string)),
		Category:         strings.TrimSpace(sccFinding["category"].(string)),
		Severity:         findingSeverity,
		FindingClass:     findingClass,
		EventTime:        eventTime,
		ExternalUri:      findingExternalUri,
		Description:      findingDescription,
		SourceProperties: sourceProperties,
	}

	var findingId string = strings.TrimSpace(sccFinding["findingId"].(string))
	req := &securitycenterpb.CreateFindingRequest{
		Parent:    config.SecurityCommandCenter.Source,
		FindingId: findingId,
		Finding:   finding,
	}

	_, err = c.CreateFinding(ctx, req)
	if err != nil {
		return fmt.Errorf("Failed to create SCC finding: %w", err)
	}

	log.Debug().Str("findingId", findingId).Msg("Security Command Center finding created!")

	return nil
}

func CreateBlockRule(config Config, detectionConfig DetectionConfig, detection Detection) error {
	ctx := context.Background()
	c, err := dns.NewService(ctx)
	if err != nil {
		return err
	}

	var projectId string
	if config.Block.ProjectID == "" {
		projectId, err = GetProjectId(&ctx)
		if err != nil {
			return err
		}
	} else {
		projectId = config.Block.ProjectID
	}

	var rulePrefix string = "dnsexfil-"
	if config.Block.RulePrefix != "" {
		rulePrefix = config.Block.RulePrefix
	}

	var ruleSuffix string = strings.ToLower(strings.TrimSuffix(detection.Domain, "."))
	ruleSuffix = strings.ReplaceAll(ruleSuffix, ".", "-")
	reg, _ := regexp.Compile("[^a-z0-9-]+")
	ruleSuffix = reg.ReplaceAllString(ruleSuffix, "")
	ruleName := fmt.Sprintf("%s%s", rulePrefix, ruleSuffix)

	var destination string = "127.0.0.1"
	if config.Block.Destination != "" {
		destination = config.Block.Destination
	}

	_, existingErr := c.ResponsePolicyRules.Get(projectId, config.Block.ResponsePolicy, ruleName).Do()

	if existingErr != nil {
		rule := dns.ResponsePolicyRule{
			RuleName: ruleName,
			DnsName:  fmt.Sprintf("*.%s", detection.Domain),
			LocalData: &dns.ResponsePolicyRuleLocalData{
				LocalDatas: []*dns.ResourceRecordSet{
					&dns.ResourceRecordSet{
						Name:    fmt.Sprintf("*.%s", detection.Domain),
						Type:    "A",
						Ttl:     60,
						Rrdatas: []string{destination},
					},
				},
			},
		}

		_, err = c.ResponsePolicyRules.Create(projectId, config.Block.ResponsePolicy, &rule).Do()
		if err != nil {
			return err
		}
		log.Info().Str("projectId", projectId).Str("domain", detection.Domain).Str("responsePolicy", config.Block.ResponsePolicy).Str("responsePolicyRule", ruleName).Msgf("Response policy rule created for %s: %s", detection.Domain, ruleName)
	} else {
		log.Info().Str("projectId", projectId).Str("domain", detection.Domain).Str("responsePolicy", config.Block.ResponsePolicy).Str("responsePolicyRule", ruleName).Msgf("Response policy rule already exists: %s", ruleName)
	}
	return nil
}

func ProcessDetections(config Config, detectionConfig DetectionConfig, detections []Detection) error {
	for _, detection := range detections {
		if detectionConfig.Action == "warn" {
			var forLog []string
			var i int = 0
			var supressed bool = false
			for _, logEntry := range detection.Queries {
				forLog = append(forLog, logEntry["Payload"].(map[string]interface{})["JsonPayload"].(map[string]interface{})["queryName"].(string))
				i += 1
				if i > 20 {
					supressed = true
					break
				}
			}
			log.Warn().Float64("entropy", detection.Entropy).Int("count", len(detection.Queries)).Str("domain", detection.Domain).Bool("allQueriesShown", !supressed).Msgf("DNS exfiltration match for: %s", detection.Domain)
		}
		if detectionConfig.Action == "block" {
			err := CreateBlockRule(config, detectionConfig, detection)
			if err != nil {
				return err
			}
		}

		if detectionConfig.SecurityCommandCenter.CreateFinding != nil && *detectionConfig.SecurityCommandCenter.CreateFinding {
			err := CreateSccFinding(config, detectionConfig, detection)
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func ProcessLogEntries(config DetectionConfig, inputLogEntries map[string][]map[string]interface{}) ([]Detection, error) {
	var detections []Detection

	var processedLogEntries map[string][]map[string]interface{}
	if config.QueryParts > 0 {
		processedLogEntries = make(map[string][]map[string]interface{}, 0)
		for queryName, queryLogEntries := range inputLogEntries {
			queryParts := strings.Split(queryName, ".")
			from := (len(queryParts) - config.QueryParts - 1)
			if from < 0 {
				from = 0
			}
			to := from - 1
			if to < 0 {
				to = 0
			}
			processedQueryName := strings.Join(queryParts[from:], ".")

			for _, logEntry := range queryLogEntries {
				logQueryName := logEntry["Payload"].(map[string]interface{})["JsonPayload"].(map[string]interface{})["queryName"].(string)
				if len(logQueryName) >= config.MinimumLength {
					logQueryParts := strings.Split(logQueryName, ".")
					logEntry["processedQueryName"] = strings.Join(logQueryParts[0:to+1], "")

					if _, ok := processedLogEntries[processedQueryName]; !ok {
						processedLogEntries[processedQueryName] = make([]map[string]interface{}, 0)
					}
					processedLogEntries[processedQueryName] = append(processedLogEntries[processedQueryName], logEntry)
				}
			}
		}
	} else {
		processedLogEntries = make(map[string][]map[string]interface{}, 0)
		for queryName, queryLogEntries := range inputLogEntries {
			if _, ok := processedLogEntries[queryName]; !ok {
				processedLogEntries[queryName] = make([]map[string]interface{}, 0)
			}
			for _, logEntry := range queryLogEntries {
				if len(queryName) >= config.MinimumLength {
					logEntry["processedQueryName"] = queryName
					processedLogEntries[queryName] = append(processedLogEntries[queryName], logEntry)
				}
			}
		}
	}

	allowAllCharacters := false
	if config.AllowAllCharacters != nil && *config.AllowAllCharacters {
		allowAllCharacters = true
	}
	for qk, logEntries := range processedLogEntries {
		var sb strings.Builder
		for _, logEntry := range logEntries {
			queryName := logEntry["processedQueryName"].(string)
			sb.WriteString(queryName)
		}

		var queryEntropy float64
		var err error
		if config.Ngrams == nil || *config.Ngrams == 1 {
			queryEntropy, err = ShannonString(sb.String(), allowAllCharacters)
			if err != nil {
				return nil, err
			}
		} else {
			queryEntropy, err = ShannonStringNgrams(sb.String(), *config.Ngrams)
			if err != nil {
				return nil, err
			}
		}

		if queryEntropy > config.BitsOfEntropy {
			log.Debug().Float64("entropy", queryEntropy).Int("count", len(logEntries)).Str("domain", qk).Msgf("Possible DNS exfiltration match for: %s", qk)
			if len(logEntries) >= config.LogEntriesRequired {
				log.Debug().Float64("entropy", queryEntropy).Int("count", len(logEntries)).Str("domain", qk).Msgf("DNS exfiltration match for: %s", qk)

				detections = append(detections, Detection{
					Entropy:            queryEntropy,
					Domain:             qk,
					NumberOfLogEntries: len(logEntries),
					Queries:            logEntries,
				})
			}
		} else {
			log.Debug().Float64("entropy", queryEntropy).Int("count", len(logEntries)).Str("domain", qk).Msgf("Does not look like DNS exfiltration for: %s", qk)
		}
	}

	return detections, nil
}

func GetProjectId(ctx *context.Context) (string, error) {
	log.Debug().Msg("Detecting current project ID...")
	credentials, err := oauth.FindDefaultCredentials(*ctx, compute.ComputeScope)
	if err != nil {
		return "", err
	}
	if credentials.ProjectID == "" {
		if os.Getenv("GOOGLE_CLOUD_PROJECT") != "" {
			return os.Getenv("GOOGLE_CLOUD_PROJECT"), nil
		}
	} else {
		return credentials.ProjectID, nil
	}
	return "", fmt.Errorf("Unable to detect current project ID!")
}

func LoadLogMessages(config Config) (map[string][]map[string]interface{}, error) {
	ctx := context.Background()
	c, err := logging.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	var filterBuf bytes.Buffer
	tr := gomplate.NewRenderer(gomplate.RenderOptions{})
	err = tr.Render(ctx, "filter", config.Logging.Filter, &filterBuf)
	if err != nil {
		return nil, err
	}

	var resourceNames []string
	if config.Logging.ResourceNames == nil {
		projectId, err := GetProjectId(&ctx)
		if err != nil {
			return nil, err
		}
		resourceNames = []string{fmt.Sprintf("projects/%s", projectId)}
	} else {
		resourceNames = config.Logging.ResourceNames
	}

	log.Info().Str("filter", filterBuf.String()).Msg("Querying log entries...")

	req := &loggingpb.ListLogEntriesRequest{
		ResourceNames: resourceNames,
		Filter:        filterBuf.String(),
	}
	if config.Logging.PageSize != nil {
		req.PageSize = int32(*config.Logging.PageSize)
	}

	// Compile allowlist globs
	var allowList []glob.Glob
	for _, domain := range config.AllowList.Domains {
		domainGlob, err := glob.Compile(domain, '.')
		if err != nil {
			return nil, err
		}
		allowList = append(allowList, domainGlob)
	}

	logEntries := make(map[string][]map[string]interface{}, 0)

	var totalLogEntries int = 0
	it := c.ListLogEntries(ctx, req)
NEXT_ENTRY:
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}

		var logEntryBytes []byte
		var logEntry map[string]interface{}
		logEntryBytes, err = json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(logEntryBytes, &logEntry)
		if err != nil {
			return nil, err
		}

		logEntry["timestamp"] = resp.Timestamp.AsTime()
		logEntry["receive_timestamp"] = resp.ReceiveTimestamp.AsTime()

		totalLogEntries += 1

		if _, ok := logEntry["Payload"]; ok {
			if _, ok := logEntry["Payload"].(map[string]interface{})["JsonPayload"]; ok {
				if queryName, ok := logEntry["Payload"].(map[string]interface{})["JsonPayload"].(map[string]interface{})["queryName"].(string); ok {

					// Check for allow list
					for _, allowDomain := range allowList {
						if allowDomain.Match(queryName) {
							continue NEXT_ENTRY
						}
					}

					if _, ok := logEntries[queryName]; !ok {
						logEntries[queryName] = make([]map[string]interface{}, 0)
					}
					logEntries[queryName] = append(logEntries[queryName], logEntry)

				} else {
					return nil, &LogError{Message: "log entry had no Payload.JsonPayload.queryName field", LogEntry: logEntry}
				}
			} else {
				return nil, &LogError{Message: "log entry had no Payload.JsonPayload field", LogEntry: logEntry}
			}
		} else {
			return nil, &LogError{Message: "log entry had no Payload field", LogEntry: logEntry}
		}
	}

	log.Info().Int("logentries", totalLogEntries).Msgf("Read %d log entries.", totalLogEntries)

	return logEntries, nil
}

func ShannonStringNgrams(s string, n int) (float64, error) {
	var slen int = len(s)

	// Not even with 3, add some padding
	if slen%n != 0 {
		s += strings.Repeat("-", slen%n)
	}

	var total int = 0
	ngrams := make(map[string]int)
	for i := 0; i < slen; i += n {
		ngrams[s[i:i+n]]++
		total++
	}

	var entropy float64 = 0.0
	for _, count := range ngrams {
		x := float64(count) / float64(total)
		if x > 0.0 {
			entropy += -x * math.Log2(x)
		}
	}
	return entropy, nil
}

func ShannonString(s string, allowAllCharacters bool) (float64, error) {
	RuneFreq := make(map[rune]int)
	if allowAllCharacters {
		for i := 32; i <= 127; i++ {
			RuneFreq[rune(i)] = 0
		}
	} else {
		for i := 0; i < 256; i++ {
			RuneFreq[rune(i)] = 0
		}
	}

	var totalRunes = len(s)
	for _, r := range s {
		if !allowAllCharacters {
			if int(r) < 32 || int(r) > 127 {
				return 0, fmt.Errorf("invalid rune encountered when all characters not allowed: %d", int(r))
			}
		}
		RuneFreq[r] += 1
		totalRunes += 1
	}

	var entropy float64 = 0.0
	for _, count := range RuneFreq {
		x := float64(count) / float64(totalRunes)
		if x > 0.0 {
			entropy += -x * math.Log2(x)
		}
	}
	return entropy, nil
}
