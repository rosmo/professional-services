# Cloud Instance Mapper

This tool creates a score-based mapping of different IaaS VM instance types from EC2 and Azure Compute
to Google Cloud Platform instance types.

When set up correctly with authentication, it will retrieve the latest instance type details from
each of the cloud providers's APIs. 

## Building

* Requires Golang 1.16+

You can build it by running:

```sh
go get github.com/GoogleCloudPlatform/professional-services/tools/instance_mapper
```

## Running

Example:

```sh
instance_mapper 
  -azure-vm \
  -azure-subscription-id 5ed066d4-80cb-4b57-ba9e-5a64e6ee3f05 \
  -aws-ec2 \
  -aws-role arn:aws:iam::12345678:role/EC2ReadOnly \
  -gcp-project my-project-id \
  -save-file cache.yaml \
  -load-file cache.yaml \
  > instances.csv
```

The tool outputs a CSV formatted file on standard out, that looks something like this:

|Instance type|Memory                       |vCPUs |GPUs                                         |GPU type|Total GPU memory|Instance type |Memory   |vCPUs|GPUs|GPU type|Total GPU memory|Instance type  |Memory   |vCPUs|GPUs|GPU type|Total GPU memory|Instance type  |Memory   |vCPUs|GPUs|GPU type|Total GPU memory|
|-------------|-----------------------------|------|---------------------------------------------|--------|----------------|--------------|---------|-----|----|--------|----------------|---------------|---------|-----|----|--------|----------------|---------------|---------|-----|----|--------|----------------|
|c5d.12xlarge |96.00 GB                     |48    |0                                            |        |0               |n2d-highcpu-96|96.00 GB |96   |0   |        |0               |n2d-standard-48|192.00 GB|48   |0   |        |0               |n2d-highmem-48 |384.00 GB|48   |0   |        |0               |
|c4.large     |3.75 GB                      |2     |0                                            |        |0               |e2-medium     |4.00 GB  |2    |0   |        |0               |e2-standard-2  |8.00 GB  |2    |0   |        |0               |e2-small       |2.00 GB  |2    |0   |        |0               |
|r4.xlarge    |30.50 GB                     |4     |0                                            |        |0               |e2-highmem-4  |32.00 GB |4    |0   |        |0               |n2d-highmem-4  |32.00 GB |4    |0   |        |0               |n2-highmem-4   |32.00 GB |4    |0   |        |0               |
|c6gd.2xlarge |16.00 GB                     |8     |0                                            |        |0               |e2-standard-4 |16.00 GB |4    |0   |        |0               |e2-standard-8  |32.00 GB |8    |0   |        |0               |e2-highcpu-16  |16.00 GB |16   |0   |        |0               |
|Standard_D1  |3.50 GB                      |1     |0                                            |        |0               |n1-standard-1 |3.75 GB  |1    |0   |        |0               |t2d-standard-1 |4.00 GB  |1    |0   |        |0               |n1-highcpu-4   |3.60 GB  |4    |0   |        |0               |
|Standard_E80is_v4|504.00 GB                    |80    |0                                            |        |0               |n2d-highmem-64|512.00 GB|64   |0   |        |0               |n2-highmem-64  |512.00 GB|64   |0   |        |0               |n2d-highmem-80 |640.00 GB|80   |0   |        |0               |
|Standard_M64s_v2|1024.00 GB                   |64    |0                                            |        |0               |m1-ultramem-40|961.00 GB|40   |0   |        |0               |n2d-highcpu-64 |64.00 GB |64   |0   |        |0               |n2d-standard-64|256.00 GB|64   |0   |        |0               |
|Standard_ND40s_v3|672.00 GB                    |40    |8                                            |        |0               |n2d-highmem-80|640.00 GB|80   |0   |        |0               |               |         |     |    |        |                |               |         |     |    |        |                |
|Standard_D16s_v3|64.00 GB                     |16    |0                                            |        |0               |e2-standard-16|64.00 GB |16   |0   |        |0               |n2d-standard-16|64.00 GB |16   |0   |        |0               |n2-standard-16 |64.00 GB |16   |0   |        |0               |


## Limitations

- Azure API does not provide: a GPU type description, shared tenancy support, bare metal,
  CPU clockspeed


