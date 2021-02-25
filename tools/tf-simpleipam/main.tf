terraform {
  required_providers {
    simpleipam = {
      source  = "github.com/GoogleCloudPlatform/simpleipam"
      version = ">= 0.1.0"
    }
  }
}

provider "simpleipam" {
  id = "test"
  //pool_file = "ipam.json"
  pool_file = "gs://your-storage-bucket/ipam.json"
}

resource "simpleipam_network" "test" {
  name       = "myalloc"
  pool       = "pods"
  pool_index = 1
  netmask    = 24
}
