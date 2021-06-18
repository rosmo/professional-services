//   Copyright 2021 Google LLC
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package main

import (
	"flag"
	"log"
	"os"
	"strings"

	metadataproxy "github.com/GoogleCloudPlatform/professional-services/tools/per-user-metadata-proxy"
)

var serviceAccountMap map[string]string

func main() {
	bindAddress := flag.String("B", "127.0.0.1:12972", "Bind address")
	flag.Parse()
	tail := flag.Args()
	if len(tail) == 0 {
		log.Fatalf("ERR: Specify at least one 'username=service-account@project.iam.gserviceaccount.com' in the command line arguments.")
	}

	log.Printf("INFO: Per User Metadata Proxy (version %s) starting...", metadataproxy.VERSION)

	serviceAccountMap = make(map[string]string, len(tail))
	for _, sa := range tail {
		if !strings.Contains(sa, "=") {
			log.Fatalf("ERR: Malformed username and service account mapping (example: 'username=serviceaccount@project.iam.gserviceaccount.com'): %s", sa)
		}
		parts := strings.SplitN(sa, "=", 2)
		serviceAccountMap[parts[0]] = parts[1]
		log.Printf("INFO: Mapping Unix user '%s' to service account '%s'.", parts[0], parts[1])
	}

	if *bindAddress == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	err := metadataproxy.StartProxy(*bindAddress, serviceAccountMap)
	if err != nil {
		log.Fatalf("ERR: Error starting proxy: %s", err.Error)
	}
}
