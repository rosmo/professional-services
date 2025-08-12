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
    -relationships-table "${GCPVIZ_RELATIONSHIPS_TABLE}" \
    -project "${GOOGLE_PROJECT}" \
    -location "${GOOGLE_REGION}" \
    --query-paramters "${GCPVIZ_QUERY_PARAMETERS}" \
    --log-level "${GCPVIZ_LOG_LEVEL}"

log "info" "Creating graph with d2..."
/usr/local/bin/d2 -w /tmp/graph.d2 /tmp/graph.svg

TSTAMP=$(date +'%Y%m%d_%H%I%S')
TARGET_FILE="gs://${GCPVIZ_RESULT_BUCKET}/${TSTAMP}.svg"
log "info" "Copying generated graph to ${TARGET_FILE}..."
gcloud storage cp /tmp/graph.svg $TARGET_FILE