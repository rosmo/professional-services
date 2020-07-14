#!/usr/bin/env python

from jsonpath_ng import jsonpath, parse
import fileinput
import json
import sys

asset_types = [
    'cloudresourcemanager.googleapis.com/Organization',
    'cloudresourcemanager.googleapis.com/Folder',
    'cloudresourcemanager.googleapis.com/Project',
    'compute.googleapis.com/Address',
    'compute.googleapis.com/GlobalAddress',
    'compute.googleapis.com/Autoscaler',
    'compute.googleapis.com/BackendBucket',
    'compute.googleapis.com/BackendService',
    'compute.googleapis.com/Disk',
    'compute.googleapis.com/Firewall',
    'compute.googleapis.com/ForwardingRule',
    'compute.googleapis.com/HealthCheck',
    'compute.googleapis.com/HttpHealthCheck',
    'compute.googleapis.com/HttpsHealthCheck',
    'compute.googleapis.com/Image',
    'compute.googleapis.com/Instance',
    'compute.googleapis.com/InstanceGroup',
    'compute.googleapis.com/InstanceGroupManager',
    'compute.googleapis.com/InstanceTemplate',
    'compute.googleapis.com/Interconnect',
    'compute.googleapis.com/InterconnectAttachment',
    'compute.googleapis.com/License',
    'compute.googleapis.com/Network',
    'compute.googleapis.com/Project',
    'compute.googleapis.com/RegionDisk',
    'compute.googleapis.com/Route',
    'compute.googleapis.com/Router',
    'compute.googleapis.com/SecurityPolicy',
    'compute.googleapis.com/Snapshot',
    'compute.googleapis.com/SslCertificate',
    'compute.googleapis.com/Subnetwork',
    'compute.googleapis.com/TargetHttpProxy',
    'compute.googleapis.com/TargetHttpsProxy',
    'compute.googleapis.com/TargetInstance',
    'compute.googleapis.com/TargetPool',
    'compute.googleapis.com/TargetTcpProxy',
    'compute.googleapis.com/TargetSslProxy',
    'compute.googleapis.com/TargetVpnGateway',
    'compute.googleapis.com/UrlMap',
    'compute.googleapis.com/VpnTunnel',
    'appengine.googleapis.com/Application',
    'appengine.googleapis.com/Service',
    'appengine.googleapis.com/Version',
    'storage.googleapis.com/Bucket',
    'osconfig.googleapis.com/PatchDeployment',
    'dns.googleapis.com/ManagedZone',
    'dns.googleapis.com/Policy',
    'spanner.googleapis.com/Instance',
    'spanner.googleapis.com/Database',
    'spanner.googleapis.com/Backup',
    'bigquery.googleapis.com/Dataset',
    'bigquery.googleapis.com/Table',
    'iam.googleapis.com/Role',
    'iam.googleapis.com/ServiceAccount',
    'pubsub.googleapis.com/Topic',
    'pubsub.googleapis.com/Subscription',
    'dataproc.googleapis.com/Cluster',
    'dataproc.googleapis.com/Job',
    'cloudkms.googleapis.com/KeyRing',
    'cloudkms.googleapis.com/CryptoKey',
    'container.googleapis.com/Cluster',
    'container.googleapis.com/NodePool',
    'sqladmin.googleapis.com/Instance',
    'bigtableadmin.googleapis.com/Cluster',
    'bigtableadmin.googleapis.com/Instance',
    'bigtableadmin.googleapis.com/Table',
    'k8s.io/Node',
    'k8s.io/Pod',
    'k8s.io/Namespace',
    'rbac.authorization.k8s.io/Role',
    'rbac.authorization.k8s.io/RoleBinding',
    'rbac.authorization.k8s.io/ClusterRole',
    'rbac.authorization.k8s.io/ClusterRoleBinding',
    'logging.googleapis.com/LogSink',
    'logging.googleapis.com/LogMetric'
]

redacted_assets = {
    'compute.googleapis.com/BackendService': ['$.resource.data.iap.oauth2ClientSecretSha256'],
    'compute.googleapis.com/HealthCheck': ['$.resource.data.tcpHealthCheck.proxyHeader', '$.resource.data.httpHealthCheck.proxyHeader', '$.resource.data.httpsHealthCheck.proxyHeader'],
    'compute.googleapis.com/TargetSslProxy': ['$.resource.data.proxyHeader'],
    'compute.googleapis.com/TargetTcpProxy': ['$.resource.data.proxyHeader'],
    'compute.googleapis.com/VpnTunnel': ['$.resource.data.sharedSecretHash'],
    'compute.googleapis.com/SecurityPolicy': ['$.resource.data.rule'],
    'pubsub.googleapis.com/Subscription': ['$.resource.data.pushConfig.pushEndpoint'],
    'k8s.io/Pod': ['$.resource.data.spec.containers[*].args', '$.resource.data.spec.containers[*].command', '$.resource.data.spec.containers[*].env']
}

for line in fileinput.input():
    asset = json.loads(line)
    if asset['asset_type'] in asset_types:
        if asset['asset_type'] in redacted_assets:
            for jp in redacted_assets[asset['asset_type']]:
                jsonpath_expr = parse(jp)
                for match in jsonpath_expr.find(asset):
                    val = match.value
                    replacement = None
                    if isinstance(val, str):
                        replacement = ""
                    if isinstance(val, list):
                        replacement = []
                    if isinstance(val, dict):
                        replacement = {}
                    if isinstance(match.path, jsonpath.Fields):
                        for f in match.path.fields:
                            match.context.value[f] = replacement
            print('Redacted asset %s (type %s)' %
                  (asset['name'], asset['asset_type']), file=sys.stderr)
            print(json.dumps(asset))
        else:
            print(line.strip())
    else:
        print('Skipping unsupported asset %s (type %s)' %
              (asset['name'], asset['asset_type']), file=sys.stderr)
