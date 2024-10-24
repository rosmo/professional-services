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

variable "project_id" {
  type        = string
  description = "Project ID where to deploy the function"
}

variable "region" {
  type        = string
  description = "Region to deploy the function in"
}

variable "image" {
  type        = string
  description = "Container image for the application"
}

variable "logging_project_id" {
  type        = string
  description = "Logging project ID"
  default     = null
}

variable "dns_project_id" {
  type        = string
  description = "Cloud DNS project ID"
  default     = null
}

variable "organization_id" {
  type        = number
  description = "Organization ID"
}

variable "config_file" {
    type = string
    description = "Configuration file to use"
    default = "config.yaml"
}