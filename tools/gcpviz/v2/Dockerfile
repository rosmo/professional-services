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
FROM python:3.13-alpine

RUN apk update && apk add curl

RUN mkdir -p /opt/gcpviz
COPY gcpviz.py requirements.txt relations.yaml config.yaml cloud_run_job.sh merge_query.py /opt/gcpviz/
COPY google-cloud-icons /opt/gcpviz/

WORKDIR /opt/gcpviz
RUN pip3 install -r requirements.txt

# Install d2
ARG TARGETOS
ARG TARGETARCH
ENV D2_ARCH="${TARGETOS}-${TARGETARCH}"
RUN curl -L -o /tmp/d2.tar.gz https://github.com/terrastruct/d2/releases/download/v0.7.0/d2-v0.7.0-${D2_ARCH}.tar.gz
RUN mkdir -p /tmp/d2
RUN tar -xv --strip-components 1 -f /tmp/d2.tar.gz -C /tmp/d2
RUN cp /tmp/d2/bin/d2 /usr/local/bin
RUN rm -rf /tmp/d2 /tmp/d2.tar.gz

# Install gcloud
ENV SDK_DIR=/usr/local/lib
RUN apk add jq ca-certificates 

RUN if [ "$TARGETARCH" = "amd64" ] ; then \
    GCLOUD_ARCH="${TARGETOS}-x86_64"; \
    elif [ "$TARGETARCH" = "arm64" ] ; then \
    GCLOUD_ARCH="${TARGETOS}-arm"; \
    fi; \
    export GCLOUD_ARCH; \
    env | grep GCLOUD_ARCH >> /etc/profile.d/gcloud-arch.env
RUN . /etc/profile.d/gcloud-arch.env
RUN curl -sL \
    "https://www.googleapis.com/storage/v1/b/cloud-sdk-release/o?prefix=google-cloud-sdk-5" \
    | jq -r ".items[].selfLink | select ( match (\"${GCLOUD_ARCH}\") )" \
    | grep '.tar.gz' \
    | tail -n1 \
    | curl -sL $(awk '{print $1"?alt=media"}') | tar xzf - -C $SDK_DIR
RUN /usr/local/lib/google-cloud-sdk/install.sh --quiet
RUN echo "export PATH=$SDK_DIR/google-cloud-sdk/bin:$PATH" | tee -a /etc/profile.d/gcloud-sdk.sh

ENV GCPVIZ_QUERY=
ENV GCPVIZ_PARTIAL_QUERY=
ENV GCPVIZ_DATASET=
ENV GCPVIZ_QUERY_PARAMETERS=
ENV GCPVIZ_CAI_TABLE=cai
ENV GCPVIZ_RELATIONSHIPS_TABLE=relationships
ENV GCPVIZ_LOG_LEVEL=INFO
ENV GCPVIZ_RESULT_BUCKET=
ENV D2_FLAGS=
ENV GOOGLE_PROJECT=
ENV GOOGLE_REGION=europe-west4

CMD ["/bin/bash", "-c", "/opt/gcpviz/cloud_run_job.sh"]