#!/bin/bash
# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

log() {
    level=$1; shift
    tstamp=$(date)
    msg="$@"
    jq -n -c -M --arg level "${level}" --arg msg "${msg}" --arg tstamp "${tstamp}" '{timestamp: $tstamp, severity: $level, msg: $msg}' >&2
    case "$level" in 
        "error") 
            exit 1
            ;;
    esac
}

cd /opt/gcpviz

export PATH=/usr/local/lib/google-cloud-sdk/bin:$PATH
export GOOGLE_USE_DEFAULT_CREDENTIALS=true

SERVICE_ACCOUNT=$(curl -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email)
log "info" "Using service account: $SERVICE_ACCOUNT"

if [ -z "${GCPVIZ_QUERY}" ] && [ -z "${GCPVIZ_PARTIAL_QUERY}" ] ; then
    log "error" "No query configuration specified, exiting!"
elif [ ! -z "${GCPVIZ_QUERY}" ] ; then
    log "info" "Using full query configuration."
    echo "${GCPVIZ_QUERY}" > query.yaml
else
    log "info" "Using partial query configuration."
    python3 merge_query.py -query-file query.yaml -query-output-file query.yaml
fi

if [ -z "${GCPVIZ_DATASET}" ] ; then
    log "error" "No dataset specified, set it in GCPVIZ_DATASET."
fi

if [ -z "${GCPVIZ_RESULT_BUCKET}" ] ; then
    log "error" "No result bucket specified, set it in GCPVIZ_RESULT_BUCKET."
fi

log "info" "Running gcpviz..."
python3 gcpviz.py -dataset "${GCPVIZ_DATASET}" \
    -diagram-file /tmp/graph.d2 \
    -cai-table "${GCPVIZ_CAI_TABLE}" \
    -relationship-table "${GCPVIZ_RELATIONSHIPS_TABLE}" \
    -project "${GOOGLE_PROJECT}" \
    -location "${GOOGLE_REGION}" \
    -query-file query.yaml \
    --query-parameters "${GCPVIZ_QUERY_PARAMETERS}" \
    --log-level "${GCPVIZ_LOG_LEVEL}"

log "info" "Creating graph with d2..."
/usr/local/bin/d2 ${D2_FLAGS} --watch=false /tmp/graph.d2 /tmp/graph.svg

if [ ! -f /tmp/graph.svg ] ; then
    log "error" "The generated graph had no results: probably the query had no results. Try the query in BigQuery console (to see the query, set GCPVIZ_LOG_LEVEL=DEBUG) and troubleshoot."
    exit 2
fi

TSTAMP=$(date +'%Y%m%d_%H%I%S')
TARGET_FILE="gs://${GCPVIZ_RESULT_BUCKET}/gcpviz-graph-${TSTAMP}.svg"
log "info" "Copying generated graph to ${TARGET_FILE}..."

export CLOUDSDK_AUTH_ACCESS_TOKEN=$(curl -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token" | jq -r '.access_token')
gcloud storage cp /tmp/graph.svg $TARGET_FILE