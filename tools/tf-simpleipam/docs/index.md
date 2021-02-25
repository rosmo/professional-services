---
page_title: "SimpleIPAM Provider"
subcategory: ""
description: "SimpleIPAM provider"
  
---

# SimpleIPAM provider

## Configuration

SimpleIPAM is a simple Terraform provider, that can allocate network IP ranges on-demand during script
execution. It's built around a JSON file that stores the available ranges for allocation and the allocated
ranges. The file can be stored on Cloud Storage (recommended) or on local filesystem.

Example of the JSON configuration file:

```json
{
  "pools": {
    "gke-master": [
      [
        "10.60.240.0-10.60.252.112/28"
      ],
      [
        "10.124.240.0-10.124.252.112/28"
      ]
    ],
    "pods": [
      [
        "10.0.0.0-10.49.192.0/18",
        "172.16.0.0-172.16.10.0/24"
      ],
      [
        "10.64.0.0-10.113.192.0/18"
      ]
    ],
    "private": [
      [
        "10.57.208.0-10.59.94.0/23"
      ],
      [
        "10.121.208.0-10.123.94.0/23"
      ]
    ]
  },
  "allocated": {}
}
```

The allocatable ranges are organized by a string-based identifier (usually describing the purpose of
the pool) and one or more IP pools (which in turn may contain one or more IP ranges). The IP pools
are usually mapped to regions (eg. region 1 and 2 could correspond to `europe-west4` and `europe-west1`).
The provider is also initialized with an ID to prevent conflicting allocation names over different
Terraform modules/scripts.

The `simpleipam_network` resource may either ask for a specific network mask or just accept any range from
the pool.

## Example Usage

```terraform
provider "simpleipam" {
  id = "test" # Script ID, used to separate identically named reservations and prevent conflicts
  pool_file = "gs://your-gcs-bucket/ipam.json" # GCS bucket location
}

resource "simpleipam_network" "test" {
  count      = 4

  name       = "my-subnets"
  pool       = "private"
  pool_index = 1
  netmask    = 24
}

output "subnets" {
    value = [for network in simpleipam_network.test: network.network_mask] // 10.0.0.0/24, 10.0.1.0/24, ...
}
```
