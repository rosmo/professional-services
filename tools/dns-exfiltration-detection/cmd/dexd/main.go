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
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gopkg.in/yaml.v3"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	dexd "github.com/rosmo/professional-services/tools/dns-exfiltration-detection"
)

var handlerConfig dexd.Config

func run(config dexd.Config) {
	log.Debug().Interface("config", config).Msg("Loading log messages...")
	logEntries, err := dexd.LoadLogMessages(config)
	if err != nil {
		log.Fatal().Err(err).Interface("config", config).Msg("Failed to load log messages")
	}

	for idx, detectionConfig := range config.Detection {
		log.Info().Interface("detection", detectionConfig).Msgf("Processing detection config #%d", idx+1)
		detections, err := dexd.ProcessLogEntries(detectionConfig, logEntries)
		if err != nil {
			log.Fatal().Err(err).Interface("detection", detectionConfig).Msg("Detection configuration failed")
		}
		if len(detections) > 0 {
			log.Info().Interface("detection", detectionConfig).Msgf("Found %d detections for for detection config #%d", len(detections), idx+1)
		} else {
			log.Info().Interface("detection", detectionConfig).Msgf("No detections found for detection config #%d", idx+1)
		}
		err = dexd.ProcessDetections(config, detectionConfig, detections)
		if err != nil {
			log.Fatal().Err(err).Interface("detection", detectionConfig).Msg("Detection processing failed")
		}
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	run(handlerConfig)
	fmt.Fprintf(w, "Done.\n")
}

func main() {
	configFilePtr := flag.String("config", "config.yaml", "Configuration file to load")
	flag.Parse()

	var config dexd.Config
	var configBytes []byte
	var err error
	if configBytes = []byte(os.Getenv("CONFIG")); len(configBytes) == 0 {
		configBytes, err = os.ReadFile(*configFilePtr)
		if err != nil {
			log.Fatal().Err(err).Msgf("Could not read configuration file: %s", *configFilePtr)
		}
	}

	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		log.Fatal().Err(err).Str("config", string(configBytes)).Msg("Could not parse YAML configuration!")
	}

	if config.LogLevel != "" {
		switch strings.ToUpper(config.LogLevel) {
		case "PANIC":
			zerolog.SetGlobalLevel(zerolog.PanicLevel)
		case "FATAL":
			zerolog.SetGlobalLevel(zerolog.FatalLevel)
		case "ERROR":
			zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		case "WARN":
			zerolog.SetGlobalLevel(zerolog.WarnLevel)
		case "INFO":
			zerolog.SetGlobalLevel(zerolog.InfoLevel)
		case "DEBUG":
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
		case "TRACE":
			zerolog.SetGlobalLevel(zerolog.TraceLevel)
		}
	}

	if config.AllowList.FromSecretManager != "" {
		ctx := context.Background()
		c, err := secretmanager.NewClient(ctx)
		if err != nil {
			log.Fatal().Err(err).Str("config", string(configBytes)).Msg("Could not load additional allow list from Secret Manager!")
		}
		defer c.Close()

		req := &secretmanagerpb.AccessSecretVersionRequest{
			Name: config.AllowList.FromSecretManager,
		}

		result, err := c.AccessSecretVersion(ctx, req)
		if err != nil {
			log.Fatal().Err(err).Str("config", string(configBytes)).Msg("Could not load additional allow list from Secret Manager!")
		}

		var additionalAllowList []string
		err = yaml.Unmarshal(result.Payload.Data, &additionalAllowList)
		if err != nil {
			log.Fatal().Err(err).Str("allowlist", string(result.Payload.Data)).Msg("Could not parse additional allow list configuration!")
		}
		log.Info().Int("addditionalAllowList", len(additionalAllowList)).Msg("Loaded additional allow list entries from Secret Manager.")

		for _, entry := range additionalAllowList {
			config.AllowList.Domains = append(config.AllowList.Domains, entry)
		}
	}

	// Check if we are running in a Cloud Function
	if os.Getenv("PORT") != "" {
		handlerConfig = config
		http.HandleFunc("/", handler)
		port := os.Getenv("PORT")
		log.Debug().Str("port", port).Msgf("Listening on port %s", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal().Err(err).Msg("Failed to listen!")
		}
	} else {
		run(config)
	}
}
