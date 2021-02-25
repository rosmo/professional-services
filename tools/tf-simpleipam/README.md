# Simple IPAM provider for Terraform

SimpleIPAM is a simple Terraform provider, that can allocate network IP ranges on-demand during script
execution. It's built around a JSON file that stores the available ranges for allocation and the allocated
ranges. The file can be stored on Cloud Storage (recommended) or on local filesystem.

The allocatable ranges are organized by a string-based identifier (usually describing the purpose of
the pool) and one or more IP pools (which in turn may contain one or more IP ranges). The IP pools
are usually mapped to regions (eg. region 1 and 2 could correspond to `europe-west4` and `europe-west1`).
The provider is also initialized with an ID to prevent conflicting allocation names over different
Terraform modules/scripts.

The `simpleipam_network` resource may either ask for a specific network mask or just accept any range from
the pool.

