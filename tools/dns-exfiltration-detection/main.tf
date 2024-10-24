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

locals {
  iam_project_id = var.logging_project_id == null ? var.project_id : var.logging_project_id
  dns_project_id = var.dns_project_id == null ? var.project_id : var.dns_project_id
}

module "service-account" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account"
  project_id = var.project_id
  name       = "dns-exfiltration-detector"
  iam        = {}
  iam_project_roles = local.iam_project_id == local.dns_project_id ? {
    (local.iam_project_id) = [
      "roles/logging.viewer",
      "roles/dns.admin",
    ]
    } : {
    (local.iam_project_id) = [
      "roles/logging.viewer",
    ]
    (local.iam_project_id) = [
      "roles/dns.admin",
    ]
  }
  iam_organization_roles = {
    "${var.organization_id}" = [
      "roles/securitycenter.findingsEditor"
    ]
  }
}

module "invoker-service-account" {
  source            = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account"
  project_id        = var.project_id
  name              = "dns-exfiltration-invoker"
  iam               = {}
  iam_project_roles = {}
}

module "cloud-run" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/cloud-run-v2"
  project_id = var.project_id
  name       = "dns-exfiltration-detector"
  region     = var.region
  containers = {
    dns-exfil = {
      image = var.image
      env = {
        CONFIG = file(format("%s/%s", path.module, var.config_file))
      }
    }
  }
  service_account = module.service-account.email
  iam = {
    "roles/run.invoker" = [module.invoker-service-account.iam_email]
  }
  deletion_protection = false
}

resource "google_cloud_scheduler_job" "job" {
  name             = "dns-exfiltration-detector"
  project          = var.project_id
  region           = var.region
  description      = "Run DNS exfiltration detector on a schedule"
  schedule         = "*/10 * * * *"
  time_zone        = "Europe/Amsterdam"
  attempt_deadline = "320s"

  retry_config {
    retry_count = 1
  }

  http_target {
    http_method = "GET"
    uri         = module.cloud-run.service_uri
    oidc_token {
      service_account_email = module.invoker-service-account.email
    }
  }
}
