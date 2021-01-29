using System;
using System.Diagnostics;
using System.Threading.Tasks;
using System.Collections.Generic;
using System.Text.RegularExpressions;
using Microsoft.Extensions.Logging;
using Google.Apis.Auth.OAuth2;
using Google.Apis.Compute.v1;
using Google.Apis.CloudResourceManager.v1;
using Google.Apis.Services;
using Newtonsoft.Json;
using ByteSizeLib;

using ComputeData = Google.Apis.Compute.v1.Data;
using CrmData = Google.Apis.CloudResourceManager.v1.Data;

#nullable enable
#pragma warning disable CS1998
namespace GSnapshot
{
    public class Instances
    {
        private readonly ILogger<Runner> _logger;
        private readonly Utils _utils;
        private readonly Operations _operations;
        private readonly Disks _disks;

        public Dictionary<string, ComputeData.Instance> AllInstances = new Dictionary<string, ComputeData.Instance>();

        private ComputeService computeService = new ComputeService(new BaseClientService.Initializer
        {
            HttpClientInitializer = Utils.GetCredential(),
            ApplicationName = Utils.ApplicationName,
        });

        public Instances(ILogger<Runner> logger, Utils utils, Operations operations, Disks disks)
        {
            _logger = logger;
            _utils = utils;
            _operations = operations;
            _disks = disks;
        }

        private string GetLastPart(string s)
        {
            string[] sParts = s.Split('/');
            return sParts[sParts.Length - 1];
        }
        private string GetLastPartN(string s, int index)
        {
            string[] sParts = s.Split('/');
            return sParts[sParts.Length - index];
        }

        private async Task<Dictionary<string, ComputeData.Instance>> LoadInstancesForZoneAsync(string projectId, string zone)
        {
            Dictionary<string, ComputeData.Instance> results = new Dictionary<string, ComputeData.Instance>();
            InstancesResource.ListRequest request = computeService.Instances.List(projectId, zone);
            ComputeData.InstanceList response;
            do
            {
                response = await request.ExecuteAsync();
                if (response.Items == null)
                {
                    continue;
                }
                foreach (ComputeData.Instance instance in response.Items)
                {
                    results[instance.Name] = instance;
                }
                request.PageToken = response.NextPageToken;
            } while (response.NextPageToken != null);
            return results;
        }

        public async void LoadInstances(string projectId, ICollection<string> regions, Dictionary<string, List<string>> AllZones)
        {
            var tasks = new List<Task<Dictionary<string, ComputeData.Instance>>>();
            foreach (string region in regions)
            {
                _logger.LogDebug($"Loading instances in region {region}...");
                foreach (string zone in AllZones[region])
                {
                    _logger.LogDebug($"Loading instances in zone {zone}...");
                    tasks.Add(Task.Run(async () =>
                    {
                        return await LoadInstancesForZoneAsync(projectId, zone);
                    }
                    ));
                }
            }
            var finalResults = Task.WhenAll(tasks).GetAwaiter().GetResult();
            foreach (Dictionary<string, ComputeData.Instance> results in finalResults)
            {
                foreach (KeyValuePair<string, ComputeData.Instance> instance in results)
                {
                    AllInstances[instance.Key] = instance.Value;
                }
            }
        }


        public async Task<bool> StopInstance(string projectId, ComputeData.Instance instance)
        {
            string zone = GetLastPart(instance.Zone);
            InstancesResource.StopRequest request = computeService.Instances.Stop(projectId, zone, instance.Name);
            ComputeData.Operation response;
            response = request.Execute();
            bool done = await _operations.WaitForOperation(projectId, "zone", GetLastPart(instance.Zone), GetLastPart(response.SelfLink));
            return done;
        }

        public async Task<bool> StartInstance(string projectId, ComputeData.Instance instance)
        {
            string zone = GetLastPart(instance.Zone);
            InstancesResource.StartRequest request = computeService.Instances.Start(projectId, zone, instance.Name);
            ComputeData.Operation response;
            response = request.Execute();
            bool done = await _operations.WaitForOperation(projectId, "zone", GetLastPart(instance.Zone), GetLastPart(response.SelfLink));
            return done;
        }

        public ComputeData.Instance GetInstance(string projectId, ComputeData.Instance instance)
        {
            string zone = GetLastPart(instance.Zone);
            InstancesResource.GetRequest request = computeService.Instances.Get(projectId, zone, instance.Name);
            ComputeData.Instance response;
            response = request.Execute();
            return response;
        }

        public bool CheckInstanceRunning(string projectId, ComputeData.Instance instance, bool forceStopServer, string operation)
        {
            if (instance.Status != "RUNNING" && instance.Status != "TERMINATED")
            {
                _logger.LogCritical($"Instance state is not in a good state ({instance.Status}), waiting for 10 seconds...");
                bool statusOk = false;
                for (int i = 0; i < 10 && !statusOk; i++)
                {
                    System.Threading.Thread.Sleep(1000);
                    instance = GetInstance(projectId, instance);
                    if (instance.Status != "RUNNING" || instance.Status != "TERMINATED")
                    {
                        statusOk = true;
                    }
                }
                if (!statusOk)
                {
                    _logger.LogCritical($"Instance state is not in a good state ({instance.Status}), timed out after 10 seconds.");
                    System.Environment.Exit(5);
                }
            }
            if (instance.Status == "RUNNING")
            {
                bool stopServer = forceStopServer;
                if (!forceStopServer)
                {
                    if (operation == "snapshot")
                    {
                        _logger.LogWarning($"Instance is running, do you want to stop it first?");
                        _logger.LogInformation($"(Stopping an instance guarantees a consistent snapshot from a clean shutdown, but is not required)");
                    }
                    if (operation == "rollback")
                    {
                        _logger.LogWarning($"Instance is running, do you want proceed stopping it?");
                        _logger.LogInformation($"(Instance needs to be stopped to reattach disks from snapshots)");
                    }
                    stopServer = _utils.GetYesNo("Stop server (Y/n)?", true);
                }
                if (stopServer)
                {
                    _logger.LogWarning($"Stopping instance {instance.Name} now...");
                    bool serverStopped = StopInstance(projectId, instance).GetAwaiter().GetResult();
                    do
                    {
                        var stoppingInstance = GetInstance(projectId, instance);
                        if (stoppingInstance.Status == "TERMINATED")
                        {
                            _logger.LogInformation("Instance has been stopped successfully.");
                            break;
                        }
                        if (stoppingInstance.Status != "RUNNING" && stoppingInstance.Status != "STOPPING")
                        {
                            _logger.LogCritical($"Unknown status encountered during instance stop ({stoppingInstance.Status}), but continuing.");
                            break;
                        }
                        System.Threading.Thread.Sleep(750);
                    } while (true);
                }
                if (!stopServer && operation == "rollback")
                {
                    _logger.LogCritical("Instance needs to be stopped to reattach rolled back disks!");
                    System.Environment.Exit(6);
                }
            }
            return instance.Status == "RUNNING";
        }

        public string GetMachineDescription(ComputeData.Instance instance)
        {
            string machineType = GetLastPart(instance.MachineType);
            string disks = $"{instance.Disks.Count} disks:";
            string diskList = "";
            foreach (ComputeData.AttachedDisk disk in instance.Disks)
            {
                if (diskList != "")
                {
                    diskList += ", ";
                }
                diskList += $"{disk.DiskSizeGb} GB";
            }
            return $"{machineType}, {disks} {diskList})";
        }


#pragma warning disable CS8602
        public async Task<bool> AttachNewDisksAsync(string projectId, string snapshotPrefix, ComputeData.Instance instance, Dictionary<string, ComputeData.Disk?> disks)
        {
            foreach (ComputeData.AttachedDisk disk in instance.Disks)
            {
                string diskName = _utils.GetLastPart(disk.Source);
                string zone = _utils.GetLastPartN(disk.Source, 3);
                string diskType = _utils.GetLastPartN(disk.Source, 4);

                if (disks[diskName] == null)
                {
                    _logger.LogError($"Can't find new disk for attached disk {diskName}!");
                    continue;
                }

                _logger.LogInformation($"Detaching disk {diskName} (device name {disk.DeviceName}) from {instance.Name}...");
                InstancesResource.DetachDiskRequest request = computeService.Instances.DetachDisk(projectId, GetLastPart(instance.Zone), instance.Name, disk.DeviceName);
                ComputeData.Operation response = request.Execute();
                bool done = false;
                if (diskType == "regions")
                {
                    string region = _utils.GetLastPart(disks[diskName].Region);
                    done = await _operations.WaitForOperation(projectId, "zone", GetLastPart(instance.Zone), GetLastPart(response.SelfLink));

                    _logger.LogInformation($"Adding commit lookup label to regional disk {diskName}...");
                    var oldDisk = await _disks.GetRegionalDiskAsync(projectId, region, diskName);

                    ComputeData.RegionSetLabelsRequest labelRequestBody = new ComputeData.RegionSetLabelsRequest();
                    if (oldDisk.Labels == null)
                    {
                        labelRequestBody.Labels = new Dictionary<string, string>();
                    }
                    else
                    {
                        labelRequestBody.Labels = oldDisk.Labels;
                    }
                    labelRequestBody.Labels["gsnapshot"] = $"{snapshotPrefix}-{GetLastPart(oldDisk.Type)}-{oldDisk.Name}";
                    labelRequestBody.LabelFingerprint = oldDisk.LabelFingerprint;
                    RegionDisksResource.SetLabelsRequest labelRequest = computeService.RegionDisks.SetLabels(labelRequestBody, projectId, region, diskName);
                    ComputeData.Operation labelResponse = labelRequest.Execute();

                    done = await _operations.WaitForOperation(projectId, "region", region, GetLastPart(labelResponse.SelfLink));
                }
                else
                {
                    done = await _operations.WaitForOperation(projectId, "zone", zone, GetLastPart(response.SelfLink));

                    _logger.LogInformation($"Adding commit lookup label to zonal disk {diskName}...");
                    var oldDisk = await _disks.GetDiskAsync(projectId, zone, diskName);

                    ComputeData.ZoneSetLabelsRequest labelRequestBody = new ComputeData.ZoneSetLabelsRequest();
                    if (oldDisk.Labels == null)
                    {
                        labelRequestBody.Labels = new Dictionary<string, string>();
                    }
                    else
                    {
                        labelRequestBody.Labels = oldDisk.Labels;
                    }
                    labelRequestBody.LabelFingerprint = oldDisk.LabelFingerprint;
                    labelRequestBody.Labels["gsnapshot"] = $"{snapshotPrefix}-{_utils.GetLastPart(oldDisk.Type)}-{oldDisk.Name}";
                    DisksResource.SetLabelsRequest labelRequest = computeService.Disks.SetLabels(labelRequestBody, projectId, zone, diskName);
                    ComputeData.Operation labelResponse = labelRequest.Execute();

                    done = await _operations.WaitForOperation(projectId, "zone", zone, GetLastPart(labelResponse.SelfLink));
                }

                _logger.LogInformation($"Attaching new disk {disks[diskName].Name} to {instance.Name}...");
                ComputeData.AttachedDisk attachRequestBody = new ComputeData.AttachedDisk();
                attachRequestBody.Source = disks[diskName].SelfLink;
                attachRequestBody.Mode = disk.Mode;
                attachRequestBody.DeviceName = disk.DeviceName;
                attachRequestBody.Boot = disk.Boot;
                InstancesResource.AttachDiskRequest attachRequest = computeService.Instances.AttachDisk(attachRequestBody, projectId, GetLastPart(instance.Zone), instance.Name);
                ComputeData.Operation attachResponse = attachRequest.Execute();
                done = await _operations.WaitForOperation(projectId, "zone", zone, GetLastPart(attachResponse.SelfLink));
                _logger.LogInformation($"Disk {disks[diskName].Name} attached to {instance.Name}.");
            }
            return true;
        }
    }
}