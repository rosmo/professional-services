package main

/*
   Copyright 2021 Google LLC

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	log "github.com/golang/glog"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"google.golang.org/api/iterator"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "google.golang.org/genproto/googleapis/cloud/compute/v1"
	yaml "gopkg.in/yaml.v3"

	instance_mapper "github.com/GoogleCloudPlatform/professional-services/tools/instance_mapper"
)

var DEFAULT_MATCHER string = `
(source.total_memory.within_percentage(target.total_memory, 10) ? 10.0 : 0.0) 	        +
(source.total_memory.within_percentage(target.total_memory, 20) ? 5.0 : 0.0) 	        +
(source.total_memory.within_percentage(target.total_memory, 40) ? 2.5 : 0.0) 	        +
(source.total_vcpus.within_percentage(target.total_vcpus, 10) ? 10.0 : 0.0) 	        +
(source.total_vcpus.within_percentage(target.total_vcpus, 20) ? 5.0 : 0.0) 		        +
(source.total_vcpus.within_percentage(target.total_vcpus, 40) ? 2.5 : 0.0)		        +
(source.total_gpus > 0 ? (source.total_gpus == target.total_gpus && gpu_map[source.gpu_type] == target.gpu_type ? 20.0 : -20.0) : 0.0) +
{ "e2" : 4.0, "n2d": 3.0, "n2": 2.0, "t2d": 1.0, "m2": 2.0, "m1": 1.0, "c2": 1.0, "a2" : 1.0, "n1": 0.0, "g1": 0.0, "f1": 0.0 }[target.family]
`

type InstanceType struct {
	InstanceTypeId string  `yaml:"id"`
	InstanceFamily string  `yaml:"family"`
	Region         string  `yaml:"region"`
	Description    string  `yaml:"description",omitempty`
	BareMetal      bool    `yaml:"bare_metal"`
	SharedTenancy  bool    `yaml:"shared_tenancy"`
	GPUs           int     `yaml:"total_gpus"`
	GPUType        string  `yaml:"gpu_type",omitempty`
	GPUMemory      int     `yaml:"total_gpu_memory"`
	Memory         int     `yaml:"total_memory"`
	VCPUs          int     `yaml:"total_vcpus"`
	GHz            float64 `yaml:"cpu_clockspeed"`
	Bandwidth      float64 `yaml:"network_bandwidth"`
}

type InstanceData struct {
	AwsInstances   map[string]map[string]InstanceType `yaml:"aws"`
	GcpInstances   map[string]map[string]InstanceType `yaml:"gcp"`
	AzureInstances map[string]map[string]InstanceType `yaml:"azure"`
}

type kv struct {
	Key   string
	Value float64
}

type GPUMap struct {
	Mapping map[string]string `yaml:"gpu_mapping"`
}

type MapError struct {
	Msg string
	Err error
}

func (e *MapError) Error() string {
	return e.Msg + " (" + e.Err.Error() + ")"
}

func (it InstanceType) String() string {
	return fmt.Sprintf("%s (%d VCPUs @ %.1f GHz, %.2f GB memory, %d GPUs)", it.InstanceTypeId, it.VCPUs, it.GHz, float64(it.Memory)/1024.0, it.GPUs)
}

func (it InstanceType) ToCSV() []string {
	return []string{it.InstanceTypeId, fmt.Sprintf("%.2f GB", float64(it.Memory)/1024.0), fmt.Sprintf("%d", it.VCPUs), fmt.Sprintf("%d", it.GPUs), it.GPUType, fmt.Sprintf("%d", it.GPUMemory)}
}

func (it InstanceType) CSVHeaders() []string {
	return []string{"Instance type", "Memory", "vCPUs", "GPUs", "GPU type", "Total GPU memory"}
}

func (it InstanceType) ToMap() *map[string]interface{} {
	return &map[string]interface{}{
		"id":                it.InstanceTypeId,
		"family":            it.InstanceFamily,
		"region":            it.Region,
		"description":       it.Description,
		"bare_metal":        it.BareMetal,
		"shared_tenancy":    it.SharedTenancy,
		"total_gpus":        it.GPUs,
		"gpu_type":          it.GPUType,
		"total_gpu_memory":  it.GPUMemory,
		"total_memory":      it.Memory,
		"total_vcpus":       it.VCPUs,
		"cpu_clockspeed":    it.GHz,
		"network_bandwidth": it.Bandwidth,
	}
}

func GetGCPInstanceTypes(ctx context.Context, projectId string, debug bool) (*map[string]map[string]InstanceType, error) {
	var gcpInstances = make(map[string]map[string]InstanceType, 0)
	gcpInstances["all"] = make(map[string]InstanceType, 0)

	c, err := compute.NewMachineTypesRESTClient(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	req := &computepb.AggregatedListMachineTypesRequest{
		Project: projectId,
	}
	it := c.AggregatedList(ctx, req)
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		region := strings.TrimPrefix(resp.Key[0:len(resp.Key)-2], "zones/")
		if _, found := gcpInstances[region]; !found {
			gcpInstances[region] = make(map[string]InstanceType, 0)
		}

		for _, mt := range resp.Value.MachineTypes {
			gpus := 0
			gpuType := ""
			if len(mt.GetAccelerators()) > 0 {
				for _, accl := range mt.GetAccelerators() {
					gpus = gpus + int(*accl.GuestAcceleratorCount)
					if gpuType == "" {
						gpuType = *accl.GuestAcceleratorType
					}
				}
			}

			family := strings.SplitN(mt.GetName(), "-", 2)
			it := InstanceType{
				InstanceTypeId: mt.GetName(),
				InstanceFamily: family[0],
				Region:         region,
				Description:    mt.GetDescription(),
				BareMetal:      false,
				SharedTenancy:  strings.HasPrefix(mt.GetName(), "e2-"), // to be un-hardcoded
				GPUs:           gpus,
				GPUMemory:      0,
				GPUType:        gpuType,
				Memory:         int(mt.GetMemoryMb()),
				VCPUs:          int(mt.GetGuestCpus()),
				GHz:            3.0,
				Bandwidth:      0,
			}

			gcpInstances[it.Region][it.InstanceTypeId] = it
			gcpInstances["all"][it.InstanceTypeId] = it
			log.Infof("GCP instance type (region: %s): %s\n", it.Region, it.String())
		}

	}
	return &gcpInstances, nil
}

func GetAzureInstanceTypes(ctx context.Context, subscriptionId string, debug bool) (*map[string]map[string]InstanceType, error) {
	var azureInstances = make(map[string]map[string]InstanceType, 0)
	azureInstances["all"] = make(map[string]InstanceType, 0)

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}

	client := armcompute.NewResourceSKUsClient(subscriptionId, cred, nil)
	pager := client.List(&armcompute.ResourceSKUsListOptions{})
	for pager.NextPage(ctx) {
		for _, instance := range pager.PageResponse().ResourceSKUsResult.Value {
			it := InstanceType{
				InstanceTypeId: *instance.Name,
			}
			if instance.Family != nil {
				it.InstanceFamily = *instance.Family
			}

			supportsIaaS := false
			for _, cap := range instance.Capabilities {
				var err error
				switch name := *cap.Name; name {
				case "VMDeploymentTypes":
					if strings.Contains(*cap.Value, "IaaS") {
						supportsIaaS = true
					}
				case "vCPUs":
					it.VCPUs, err = strconv.Atoi(*cap.Value)
					it.GPUType = ""
				case "GPUs":
					it.GPUs, err = strconv.Atoi(*cap.Value)
				case "MemoryGB":
					memory, err := strconv.ParseFloat(*cap.Value, 64)
					if err == nil {
						it.Memory = int(math.Round(memory * 1024.0))
					}
				}
				if err != nil {
					return nil, err
				}
			}
			if supportsIaaS {
				for _, loc := range instance.LocationInfo {
					if _, found := azureInstances[*loc.Location]; !found {
						azureInstances[*loc.Location] = make(map[string]InstanceType, 0)
					}
					if _, found := azureInstances[*loc.Location][it.InstanceTypeId]; !found {
						azureInstances[*loc.Location][it.InstanceTypeId] = it
					}
				}
				if _, found := azureInstances["all"][it.InstanceTypeId]; !found {
					azureInstances["all"][it.InstanceTypeId] = it
				}
				log.Infof("Azure instance type: %s\n", it.String())
			}
		}

		if pager.Err() != nil {
			return nil, pager.Err()
		}
	}
	return &azureInstances, nil
}

func GetAWSInstanceTypes(ctx context.Context, role string, debug bool) (*map[string]map[string]InstanceType, error) {
	var awsInstances = make(map[string]map[string]InstanceType, 0)

	log.Info("Setting up AWS credentials...")
	var configs = []func(*config.LoadOptions) error{}
	if debug {
		configs = append(configs, config.WithClientLogMode(aws.LogRetries|aws.LogRequestWithBody))
	}
	cfg, err := config.LoadDefaultConfig(ctx, configs...)
	if err != nil {
		return nil, err
	}

	stsSvc := sts.NewFromConfig(cfg)
	creds := stscreds.NewAssumeRoleProvider(stsSvc, role)
	cfg.Credentials = aws.NewCredentialsCache(creds)

	log.Info("Fetching all AWS regions...")
	svc := ec2.NewFromConfig(cfg)
	out, err := svc.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}

	awsInstances["all"] = make(map[string]InstanceType, 0)
processRegions:
	for _, region := range out.Regions {
		log.Infof("Loading EC2 instance types from region: %s", *region.RegionName)
		awsInstances[*region.RegionName] = make(map[string]InstanceType, 0)

		regionCfg, err := config.LoadDefaultConfig(ctx, append(configs, config.WithRegion(*region.RegionName))...)
		if err != nil {
			return nil, err
		}
		regionStsSvc := sts.NewFromConfig(regionCfg)
		regionCreds := stscreds.NewAssumeRoleProvider(regionStsSvc, role)
		regionCfg.Credentials = regionCreds

		instanceSvc := ec2.NewFromConfig(regionCfg)
		ditPaginator := ec2.NewDescribeInstanceTypesPaginator(instanceSvc, &ec2.DescribeInstanceTypesInput{})
		for ditPaginator.HasMorePages() {
			output, err := ditPaginator.NextPage(ctx)
			if err != nil {
				log.Warningf("Error fetching instance types from %s: %s (skipping region...)", *region.RegionName, err.Error())
				continue processRegions
			}
			for _, instanceType := range output.InstanceTypes {
				instanceTypeId := string(instanceType.InstanceType)
				gpus := 0
				gpuMem := 0
				gpuType := ""
				if instanceType.GpuInfo != nil && len(instanceType.GpuInfo.Gpus) > 0 {
					for _, gpu := range instanceType.GpuInfo.Gpus {
						gpus = gpus + int(*gpu.Count)
						if gpuType == "" {
							gpuType = *gpu.Name
						}
					}
					gpuMem = int(*instanceType.GpuInfo.TotalGpuMemoryInMiB)
				}
				architectures := make([]string, 0)
				for _, sa := range instanceType.ProcessorInfo.SupportedArchitectures {
					architectures = append(architectures, string(sa))
				}
				clockspeed := 0.0
				if instanceType.ProcessorInfo.SustainedClockSpeedInGhz != nil {
					clockspeed = *instanceType.ProcessorInfo.SustainedClockSpeedInGhz
				}

				family := strings.SplitN(instanceTypeId, ".", 2)
				it := InstanceType{
					InstanceTypeId: instanceTypeId,
					InstanceFamily: family[0],
					Region:         *region.RegionName,
					Description:    strings.Join(architectures, ", "),
					BareMetal:      *instanceType.BareMetal,
					SharedTenancy:  !*instanceType.DedicatedHostsSupported,
					GPUs:           gpus,
					GPUMemory:      gpuMem,
					GPUType:        gpuType,
					Memory:         int(*instanceType.MemoryInfo.SizeInMiB),
					VCPUs:          int(*instanceType.VCpuInfo.DefaultVCpus),
					GHz:            clockspeed,
					Bandwidth:      0,
				}
				awsInstances[it.Region][it.InstanceTypeId] = it
				awsInstances["all"][it.InstanceTypeId] = it
				log.Infof("AWS instance type: %s\n", it.String())
			}
		}
	}
	return &awsInstances, nil
}

func MapInstances(csvWriter *csv.Writer, source *map[string]map[string]InstanceType, target *map[string]map[string]InstanceType, matcher cel.Program, gpuMap GPUMap, numberOfResults int) error {
	firstResult := true
	for _, sourceInstance := range (*source)["all"] {
		var scores map[string]float64 = make(map[string]float64, 0)
		for targetInstanceType, targetInstance := range (*target)["all"] {
			out, _, err := matcher.Eval(map[string]interface{}{
				"source":  *sourceInstance.ToMap(),
				"target":  *targetInstance.ToMap(),
				"gpu_map": gpuMap.Mapping,
			})
			if err != nil {
				log.Infof("CEL evaluation error, source=%v, target=%v\n", sourceInstance.ToMap(), targetInstance.ToMap())
				return &MapError{Msg: fmt.Sprintf("Failed to evaluate CEL (source: %s, target: %s)", sourceInstance.InstanceTypeId, targetInstance.InstanceTypeId), Err: err}
			}
			score := out.ConvertToType(types.DoubleType).(types.Double)
			if score > 0.0 {
				scores[targetInstanceType] = float64(score)
			}
		}
		var ss []kv
		for k, v := range scores {
			ss = append(ss, kv{k, v})
		}
		sort.SliceStable(ss, func(i, j int) bool {
			return ss[i].Value > ss[j].Value
		})

		// Only take top scores
		if len(ss) > numberOfResults {
			ss = ss[0:numberOfResults]
		}

		if firstResult {
			csvHeader := make([]string, 0)
			for i := 0; i <= numberOfResults; i++ {
				csvHeader = append(csvHeader, sourceInstance.CSVHeaders()...)
			}
			csvWriter.Write(csvHeader)
			firstResult = false
		}

		csvOutput := sourceInstance.ToCSV()
		for _, v := range ss {
			csvOutput = append(csvOutput, (*target)["all"][v.Key].ToCSV()...)
		}
		csvWriter.Write(csvOutput)
	}
	return nil
}

func main() {
	var (
		gcpInstances   *map[string]map[string]InstanceType
		awsInstances   *map[string]map[string]InstanceType
		azureInstances *map[string]map[string]InstanceType
	)
	processAwsEC2 := flag.Bool("aws-ec2", false, "process AWS EC2 instance types")
	processAzureVM := flag.Bool("azure-vm", false, "process Azure VM instance types")
	awsRole := flag.String("aws-role", "", "AWS role to assume via STS")
	azureSubscriptionId := flag.String("azure-subscription-id", "", "set Azure subscription ID")
	gcpProject := flag.String("gcp-project", os.Getenv("GOOGLE_PROJECT_ID"), "GCP project ID")
	customMatcher := flag.String("custom-matcher", "", "use a custom CEL matcher (file)")
	gpuMapping := flag.String("gpu-mapping", "gpu-mapping.yaml", "use GPU mapping file")
	save := flag.String("save-file", "", "file to save data into")
	load := flag.String("load-file", "", "file to load data from")
	debug := flag.Bool("debug", false, "enable debugging")
	numberOfResults := flag.Int("num-results", 3, "amount of matches to return")
	flag.Parse()

	log.Infoln("Cloud Instance Mapper 1.0")
	if !*processAwsEC2 && !*processAzureVM {
		log.Errorln("Specify AWS EC2 and/or Azure VM processing with command line flags.")
		os.Exit(1)
	}

	ctx := context.TODO()

	if *load != "" {
		instanceData := InstanceData{}

		log.Infof("Loading instance data from: %s", *load)
		yamlFile, err := ioutil.ReadFile(*load)
		if err != nil {
			log.Fatal(err)
		}

		err = yaml.Unmarshal(yamlFile, &instanceData)
		if err != nil {
			log.Fatal(err)
		}
		gcpInstances = &instanceData.GcpInstances
		awsInstances = &instanceData.AwsInstances
		azureInstances = &instanceData.AzureInstances
	} else {
		var err error
		if *processAzureVM {
			log.Infoln("Fetching Azure instance types...")
			azureInstances, err = GetAzureInstanceTypes(ctx, *azureSubscriptionId, *debug)
			if err != nil {
				log.Fatalln(err.Error())
			}
		}
		log.Infoln("Fetching GCP instance types...")
		gcpInstances, err = GetGCPInstanceTypes(ctx, *gcpProject, *debug)
		if err != nil {
			log.Fatal(err)
		}
		if *processAwsEC2 {
			if *awsRole == "" {
				log.Fatalln("AWS role must be defined!")
			}
			log.Infoln("Fetching AWS instance types...")
			awsInstances, err = GetAWSInstanceTypes(ctx, *awsRole, *debug)
			if err != nil {
				log.Fatalln(err.Error())
			}
		}
	}

	gpuMap := GPUMap{}
	if *gpuMapping != "" {
		log.Infof("Loading GPU mapping data from: %s", *gpuMapping)
		yamlFile, err := ioutil.ReadFile(*gpuMapping)
		if err != nil {
			log.Fatal(err)
		}

		err = yaml.Unmarshal(yamlFile, &gpuMap)
		if err != nil {
			log.Fatal(err)
		}
	}

	env, err := instance_mapper.GetEnv()
	if err != nil {
		log.Fatal(err)
	}

	matcher := DEFAULT_MATCHER
	if *customMatcher != "" {
		b, err := ioutil.ReadFile(*customMatcher)
		if err != nil {
			log.Fatalln(err)
		}
		matcher = string(b)
	}
	ast, iss := env.Compile(matcher)
	if iss.Err() != nil {
		log.Fatalf("Encountered error when compiling CEL: %s\n", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		log.Fatalf("Encountered error when processing CEL: %s\n", err.Error())
	}

	csvWriter := csv.NewWriter(os.Stdout)
	if *processAwsEC2 {
		log.Infof("Mapping AWS instances to GCP instances...")
		err := MapInstances(csvWriter, awsInstances, gcpInstances, prg, gpuMap, *numberOfResults)
		if err != nil {
			log.Fatalln(err)
		}
	}
	if *processAzureVM {
		log.Infof("Mapping Azure instances to GCP instances...")
		err := MapInstances(csvWriter, azureInstances, gcpInstances, prg, gpuMap, *numberOfResults)
		if err != nil {
			log.Fatalln(err)
		}
	}

	if *save != "" {
		instanceData := InstanceData{}
		if awsInstances != nil {
			instanceData.AwsInstances = *awsInstances
		}
		if gcpInstances != nil {
			instanceData.GcpInstances = *gcpInstances
		}
		if azureInstances != nil {
			instanceData.AzureInstances = *azureInstances
		}
		yamlData, err := yaml.Marshal(&instanceData)
		if err != nil {
			log.Fatal(err)
		}

		err = ioutil.WriteFile(*save, yamlData, 0644)
		if err != nil {
			log.Fatal(err)
		}
		log.Infof("Instance data saved to: %s\n", *save)
	}

}
