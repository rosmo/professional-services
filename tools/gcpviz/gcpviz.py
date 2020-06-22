#!/usr/bin/env python3
import sys
import json
import os
import code
import argparse
import urllib.parse
from ruruki.graphs import Graph
import pyfiglet
import yaml

supported_asset_types = [
    'cloudresourcemanager.googleapis.com/Organization',
    'cloudresourcemanager.googleapis.com/Folder',
    'cloudresourcemanager.googleapis.com/Project',
    'compute.googleapis.com/Network',
    'compute.googleapis.com/Subnetwork',
    'compute.googleapis.com/Instance',
    'compute.googleapis.com/Router',
    'compute.googleapis.com/VpnTunnel',
    'compute.googleapis.com/TargetVpnGateway',
    'compute.googleapis.com/Interconnect',
    'compute.googleapis.com/InterconnectAttachment',
]

links = {
    'folder': 'https://console.cloud.google.com/cloud-resource-manager?folder=%s',
    'project': 'https://console.cloud.google.com/home/dashboard?project=%s',
    'network': 'https://console.cloud.google.com/networking/networks/details/%s?project=%s',
    'router': 'https://console.cloud.google.com/hybrid/routers/details/%s/%s?project=%s',
    'vpntunnel': 'https://console.cloud.google.com/hybrid/vpn/tunnels/details/%s/%s?project=%s',
    'interconnectattachment': 'https://console.cloud.google.com/hybrid/attachments/details/%s/%s?project=%s',
    'instance': 'https://console.cloud.google.com/compute/instancesDetail/zones/%s/instances/%s?project=%s'
}

styles = {
    'shapes': {
        'organization': 'box',
        'folder': 'folder',
        'project': 'note',
        'network': 'component',
        'subnetwork': 'record',
        'instance': 'tab',
        'router': 'ellipse',
        'nat': 'house',
        'bgp': 'record',
        'vpntunnel': 'box',
        'interconnectattachment': 'box'
    },
    'edges': {
        'folder_to_organization': 'arrowhead=none,penwidth=3',
        'project_to_folder': 'arrowhead=none,penwidth=2',
        'router_to_network': 'arrowhead=none,style="dashed"',
        'router_to_bgp': 'arrowhead=none',
        'instance_to_network': 'style=dashed,arrowhead=none',
        'instance_to_project': 'arrowhead=dot',
        'subnetwork_to_network': 'arrowhead=none',
        'vpntunnel_to_router': 'taillabel="%s",arrowhead=none',
        'interconnectattachment_to_router': 'arrowhead=none',
        'network_to_peer': 'style=dotted,dir="both",penwidth=2',
        'network_to_project': ''
    },
    'graph': 'dpi=160,labelloc="t",labeljust="l",fontname="Google Sans",fontsize=16',
    'node': 'fontname="Google Sans",fontsize=12',
    'edge': 'fontname="Google Sans",fontsize=10',
    'overlap': 'false',
    'splines': 'curved',
    'organization': 'style=filled,fillcolor="#4285F4",color="#4285F4",fontcolor=white',
    'folder': 'style=filled,fillcolor="#F4B400",color="#F4B400",fontcolor=white',
    'project': 'style=filled,fillcolor="#0F9D58",color="#0F9D58",fontcolor=white',
    'network': 'fontcolor=black,style=filled,fillcolor="#DDDDDD"',
    'subnetwork': 'fontcolor=black',
    'instance': 'fontcolor=black',
    'router': 'fontcolor=black,style=filled,fillcolor="#D2E3FC"',
    'nat': 'fontcolor=black,style=filled,fillcolor="#E8F0FE"',
    'bgp': 'fontcolor=black',
    'vpntunnel': 'fontcolor=black,style="filled",fillcolor="#AECBFA"',
    'interconnectattachment': 'fontcolor=black,style="filled",fillcolor="#AECBFA"',
}

# Arguments
parser = argparse.ArgumentParser(
    description='Visualize GCP environments from Cloud Asset Inventory')
parser.add_argument(
    'mode', help='Operation mode: generate (generate graph file), visualize (create dot file for Graphviz)')
parser.add_argument(
    '--file', help='Graph file name (default gcpviz.graph)', default='gcpviz.graph')
parser.add_argument(
    '--input', help='Asset inventory input name (default resource_inventory.json)', default='resource_inventory.json')
parser.add_argument(
    '--title', help='Title of the diagram', default='')
parser.add_argument(
    '--only_peered_vpc_projects', help='Visualize only projects and networks that are VPC peered', action='store_true', default=False)
parser.add_argument(
    '--no_empty_projects', help='Supress empty projects', action='store_true', default=False)
parser.add_argument(
    '--no_empty_folders', help='Supress empty folders', action='store_true', default=False)
parser.add_argument(
    '--include_networks', help='Include networks', action='store_true', default=False)
parser.add_argument(
    '--no_default_networks', help='Skip "default" networks', action='store_true', default=False)
parser.add_argument(
    '--include_subnets', help='Include subnetworks with IP ranges', action='store_true', default=False)
parser.add_argument(
    '--include_vms', help='Include VM instances in project (can be repeated or specify "all")', nargs='*', default=None)
parser.add_argument(
    '--link_to_network', help='Link VMs to networks in graph', action='store_true', default=False)
parser.add_argument(
    '--include_folder', help='Include folder (can be repeated; if specified, only listed folders will be listed)', nargs='*', default=None)
parser.add_argument(
    '--include_routers', help='Show Cloud Routers, eg. VPN and Interconnect (no/yes/full)', default='no')
parser.add_argument(
    '--no_nat_routers', help='Hide Cloud NAT', action='store_true', default=False)
parser.add_argument(
    '--subgraphs', help='Create subgraphs (no, project)', default='no')
parser.add_argument(
    '--styles', help='Use custom styles from YAML file', default=None)

parser.add_argument(
    '--no_banner', help='Hide application banner', action='store_true', default=False)

args = parser.parse_args()

only_peered_vpc_projects = args.only_peered_vpc_projects
include_subnets = args.include_subnets
include_networks = args.include_networks
include_folders = []
if args.include_folder and len(args.include_folder) > 0:
    for folder in args.include_folder:
        include_folders.append('folders/%s' % (folder))
include_vms = args.include_vms if args.include_vms else []
link_to_network = args.link_to_network
subgraphs = args.subgraphs
include_routers = args.include_routers
no_nat_routers = args.no_nat_routers
no_empty_projects = args.no_empty_projects
no_empty_folders = args.no_empty_folders
no_default_networks = args.no_default_networks
peerings = []
all_networks = []

if args.mode not in ['visualize', 'generate']:
    print('Unknown operation mode (%s)' % args.mode, file=sys.stderr)
    sys.exit(1)

graph = Graph()
# Print application banner
if not args.no_banner:
    print(pyfiglet.figlet_format("gcpviz", font="slant"), file=sys.stderr)

if args.styles:
    with open(args.styles, 'r') as stream:
        custom_styles = yaml.safe_load(stream)
        print('Using custom style: %s' % (args.styles), file=sys.stderr)
        for k, v in custom_styles['styles'].items():
            if k == 'edges' or k == 'shapes':
                for kk, vv in v.items():
                    styles[k][kk] = vv
            elif k == 'nodes':
                for kk, vv in v.items():
                    styles[kk] = vv
            else:
                styles[k] = v


def format_label(label):
    return label.replace('\n', '\\n')


if args.mode == 'visualize':
    print('Reading graph from %s...' % (args.file), file=sys.stderr)
    graph.load(open(args.file))
    print('Graph loaded.', file=sys.stderr)

if args.mode == 'generate':
    print('Reading input from %s...' % (args.input), file=sys.stderr)
    graph.add_vertex_constraint('resource', 'name')

    vertexes = []
    with open(args.input, "r") as inventory_file:
        # Create nodes
        for line in inventory_file:
            resource = json.loads(line.strip())
            if resource['asset_type'] not in supported_asset_types:
                continue

            if 'data' in resource['resource']:
                data = resource['resource']['data']

            project_ids = [a for a in resource['ancestors']
                           if a.startswith('projects/')]
            if len(project_ids) > 0:
                project_id = project_ids[0][9:]

            network = ''
            if 'network' in resource['resource']['data']:
                network = resource['resource']['data']['network']

            vertex = graph.add_vertex(
                'resource', name=resource['name'], type=resource['asset_type'], ancestors=resource['ancestors'], data=resource['resource']['data'], network=network)

    with open(args.input, "r") as inventory_file:
        # Create nodes
        for line in inventory_file:
            resource = json.loads(line.strip())
            if resource['asset_type'] not in supported_asset_types:
                continue

            if 'data' in resource['resource']:
                data = resource['resource']['data']

            project_ids = [a for a in resource['ancestors']
                           if a.startswith('projects/')]
            if len(project_ids) > 0:
                project_id = project_ids[0][9:]

            source_vertex = graph.get_or_create_vertex(
                'resource', name=resource['name'])
            if 'parent' in resource['resource']:
                target_vertex = graph.get_or_create_vertex(
                    'resource', name=resource['resource']['parent'])

                graph.get_or_create_edge(source_vertex, "BY", target_vertex)

    print('Writing graph to %s...' % (args.file), file=sys.stderr)
    graph.dump(open(args.file, "w"))
    print('Graph written successfully.', file=sys.stderr)
    sys.exit(0)


def process_organizations(graph, organizations):
    for organization in organizations:
        print('  N_%s [shape=%s,label="%s",%s];' %
              (organization.ident, styles['shapes']['organization'], format_label(organization.properties['data']['displayName']), styles['organization']))

        folders = organization.get_in_vertices(
            'resource', type__eq='cloudresourcemanager.googleapis.com/Folder')
        process_folders(graph, organization, folders)

        projects = organization.get_in_vertices(
            'resource', type__eq='cloudresourcemanager.googleapis.com/Project').all()
        process_projects(graph, organization, projects)


def process_instances(graph, parent, instances):
    for instance in instances:
        machine_type = instance.properties['data']['machineType'].split('/')

        network_if = instance.properties['data']['networkInterfaces'][0]

        zone = instance.properties['data']['zone'].split('/')
        url = links['instance'] % (urllib.parse.quote_plus(zone[-1]), urllib.parse.quote_plus(
            instance.properties['data']['name']), urllib.parse.quote_plus(parent.properties['data']['projectId']))

        print('  N_%s [shape=%s,label="%s",URL="%s",%s];' %
              (instance.ident, styles['shapes']['instance'], format_label('%s\n%s\n%s' % (instance.properties['data']['name'], machine_type[-1], network_if['networkIP'])), url, styles['instance']))

        print('  N_%s -> N_%s [%s];' %
              (instance.ident, parent.ident, styles['edges']['instance_to_project']))

        if include_networks and link_to_network:
            network_name = network_if['network'].replace(
                'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')

            network = graph.get_vertices(
                'resource', type__eq='compute.googleapis.com/Network', name__eq=network_name).all()
            if len(network) > 0:
                print('  N_%s -> N_%s [%s];' %
                      (instance.ident, network[0].ident, styles['edges']['instance_to_network']))
    return True if len(instances) > 0 else False


def process_subnetworks(graph, parent, subnetworks):
    subnet_label = '{'
    subnet = None
    for subnet in subnetworks:
        if subnet_label != '{':
            subnet_label += '|'
        secondary_label = ''
        if 'secondaryIpRanges' in subnet.properties['data'] and len(subnet.properties['data']['secondaryIpRanges']) > 0:
            secondary_ranges = ''
            for secondary in subnet.properties['data']['secondaryIpRanges']:
                if secondary_ranges != '':
                    secondary_ranges += ',\n'
                secondary_ranges += secondary['ipCidrRange']
            secondary_label += ' (%s)' % (secondary_ranges)
        if secondary_label != '':
            secondary_label = '\n' + secondary_label
        subnet_label += '{%s|%s%s}' % (
            subnet.properties['data']['name'], subnet.properties['data']['ipCidrRange'], secondary_label)
    if subnet:
        subnet_label += '}'
        print('  N_%s [shape=%s,rankdir="RL",label="%s",%s];' %
              (subnet.ident, styles['shapes']['subnetwork'], format_label(subnet_label), styles['subnetwork']))

        print('  N_%s -> N_%s [%s];' %
              (subnet.ident, parent.ident, styles['edges']['subnetwork_to_network']))


def process_routers(graph, parent, networks, routers):
    for router in routers:
        if 'nats' in router.properties['data']:
            if not no_nat_routers:
                region = router.properties['data']['region'].split('/')

                nat_label = ''
                for nat in router.properties['data']['nats']:
                    if nat_label != '':
                        nat_label += ', '
                    nat_label += nat['name']

                print('  N_%s [shape=%s,label="%s",%s];' %
                      (router.ident, styles['shapes']['nat'], format_label('%s\n%s\n%s' % (router.properties['data']['name'], nat_label, region[-1])), styles['nat']))

                target_network_id = router.properties['data']['network'].replace(
                    'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')
                target_networks = parent.get_in_vertices(
                    'resource', type__eq='compute.googleapis.com/Network', name__eq=target_network_id).all()
                for network in target_networks:
                    print('  N_%s -> N_%s [%s];' %
                          (router.ident, network.ident, styles['edges']['router_to_network']))

        else:
            region = router.properties['data']['region'].split('/')
            url = links['router'] % (urllib.parse.quote_plus(region[-1]), urllib.parse.quote_plus(
                router.properties['data']['name']), urllib.parse.quote_plus(parent.properties['data']['projectId']))

            description = router.properties['data']['description'] if 'description' in router.properties['data'] else region[-1]
            print('  N_%s [shape=%s,label="%s",URL="%s",%s];' %
                  (router.ident, styles['shapes']['router'], format_label('%s\n%s' % (router.properties['data']['name'], description)), url, styles['router']))

            target_network_id = router.properties['data']['network'].replace(
                'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')
            target_networks = parent.get_in_vertices(
                'resource', type__eq='compute.googleapis.com/Network', name__eq=target_network_id).all()
            for network in target_networks:
                print('  N_%s -> N_%s [%s];' %
                      (router.ident, network.ident, styles['edges']['router_to_network']))

            if 'interfaces' in router.properties['data']:
                for interface in router.properties['data']['interfaces']:
                    if 'linkedVpnTunnel' in interface:  # VPN
                        linked_tunnel = interface['linkedVpnTunnel'].replace(
                            'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')

                        tunnels = parent.get_in_vertices(
                            'resource', type__eq='compute.googleapis.com/VpnTunnel', name__eq=linked_tunnel).all()
                        for tunnel in tunnels:
                            region = tunnel.properties['data']['region'].split(
                                '/')
                            url = links['vpntunnel'] % (urllib.parse.quote_plus(region[-1]), urllib.parse.quote_plus(
                                tunnel.properties['data']['name']), urllib.parse.quote_plus(parent.properties['data']['projectId']))

                            print('  N_%s [shape=%s,style=rounded,label="%s",URL="%s",%s];' %
                                  (tunnel.ident, styles['shapes']['vpntunnel'], format_label('%s\n%s' % (tunnel.properties['data']['name'], tunnel.properties['data']['description'])), url, styles['vpntunnel']))
                            print('  N_%s -> N_%s [%s];' %
                                  (tunnel.ident, router.ident, (styles['edges']['vpntunnel_to_router'] % (format_label(tunnel.properties['data']['peerIp'])))))
                    elif 'linkedInterconnectAttachment' in interface:
                        linked_ica = interface['linkedInterconnectAttachment'].replace(
                            'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')

                        attachments = parent.get_in_vertices(
                            'resource', type__eq='compute.googleapis.com/InterconnectAttachment', name__eq=linked_ica).all()
                        for attachment in attachments:
                            region = attachment.properties['data']['region'].split(
                                '/')
                            url = links['interconnectattachment'] % (urllib.parse.quote_plus(region[-1]), urllib.parse.quote_plus(
                                attachment.properties['data']['name']), urllib.parse.quote_plus(parent.properties['data']['projectId']))

                            print('  N_%s [shape=%s,style=rounded,label="%s",URL="%s",%s];' %
                                  (attachment.ident, styles['shapes']['interconnectattachment'], format_label('%s\n%s' % (attachment.properties['data']['name'], region[-1])), url, styles['interconnectattachment']))
                            print('  N_%s -> N_%s [%s];' %
                                  (attachment.ident, router.ident, styles['edges']['interconnectattachment_to_router']))

                            # print('  N_%s -> N_%s [taillabel="%s",headlabel="%s", arrowhead=none];' %
                            #     (attachment.ident, router.ident, format_label(tunnel.properties['data']['customerRouterIpAddress']), format_label(tunnel.properties['data']['cloudRouterIpAddress'])))

            if include_routers == 'full' and 'bgp' in router.properties['data']:
                # List advertised routes
                advertised_ranges = ''
                i = 0
                if router.properties['data']['bgp']['advertiseMode'] == 'CUSTOM':
                    for range in router.properties['data']['bgp']['advertisedIpRanges']:
                        if advertised_ranges != '':
                            advertised_ranges += ', '
                            if i % 3 == 0:
                                advertised_ranges += '\n'
                        advertised_ranges += range['range']
                        i = i + 11
                else:
                    advertised_ranges = 'All subnets'

                label = '{ASN %s|%s}' % (
                    router.properties['data']['bgp']['asn'], advertised_ranges)

                print('  N_%s_BGP [shape=%s,label="%s",%s];' %
                      (router.ident, styles['shapes']['bgp'], format_label(label), styles['bgp']))

                print('  N_%s_BGP -> N_%s [%s];' %
                      (router.ident, router.ident, styles['edges']['router_to_bgp']))


def has_only_remote_peerings(network):
    data = network.properties['data']
    only_remote_peerings = True
    if 'peerings' in data and len(data['peerings']) > 0:
        for peering in data['peerings']:
            if peering['network'] in all_networks:
                only_remote_peerings = False
                break
    return only_remote_peerings


def is_empty_network(graph, network):
    network_id = network.properties['name'].replace(
        '//compute.googleapis.com/', 'https://www.googleapis.com/compute/v1/')
    has_content = graph.get_vertices(
        'resource', type__ne='compute.googleapis.com/Subnetwork', network__eq=network_id).all()
    return has_content != None


def process_networks(graph, parent, networks):

    # Very inefficient but I'll let it slide
    all_projects = graph.get_vertices(
        'resource', type__eq='cloudresourcemanager.googleapis.com/Project').all()
    all_project_ids = []
    for project in all_projects:
        all_project_ids.append(project.properties['data']['projectId'])

    has_org_peerings = False
    for network in networks:
        data = network.properties['data']

        if no_default_networks and data['name'] == 'default':
            continue

        only_remote_peerings = has_only_remote_peerings(network)
        if not only_remote_peerings:
            has_org_peerings = True

        if not only_remote_peerings or not only_peered_vpc_projects:
            url = links['network'] % (urllib.parse.quote_plus(
                network.properties['data']['name']), urllib.parse.quote_plus(parent.properties['data']['projectId']))

            if include_networks:
                has_org_peerings = True
                print('  N_%s [shape=%s,label="%s",URL="%s",%s];' %
                      (network.ident, styles['shapes']['network'], format_label('Network:\n%s' % network.properties['data']['name']), url, styles['network']))

                print('  N_%s -> N_%s [%s];' % (network.ident,
                                                parent.ident, styles['edges']['network_to_project']))

            if include_networks and include_subnets and not network.properties['data']['autoCreateSubnetworks']:
                subnetworks = parent.get_in_vertices(
                    'resource', type__eq='compute.googleapis.com/Subnetwork').all()
                for subnet in subnetworks:
                    network_name = subnet.properties['data']['network'].split(
                        '/')
                    if network_name[-1] != network.properties['data']['name']:
                        subnetworks.remove(subnet)

                process_subnetworks(graph, network, subnetworks)

            if 'peerings' in data and len(data['peerings']) > 0:
                for peering in data['peerings']:
                    peering_id = [network.properties['name'], peering['network'].replace(
                        'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')]
                    peering_id.sort()
                    if '|'.join(peering_id) in peerings:
                        continue
                    peerings.append('|'.join(peering_id))

                    target_network = peering['network'].split('/')
                    if include_networks and target_network[6] in all_project_ids:
                        target_network_id = peering['network'].replace(
                            'https://www.googleapis.com/compute/v1/', '//compute.googleapis.com/')
                        target_network_vertexes = graph.get_vertices(
                            'resource', type__eq='compute.googleapis.com/Network', name__eq=target_network_id).all()
                        for vertex in target_network_vertexes:
                            print('  N_%s -> N_%s [%s];' %
                                  (network.ident, vertex.ident, styles['edges']['network_to_peer']))

    return has_org_peerings


def process_projects(graph, parent, projects):
    processed_one_project = False
    for project in projects:
        project_had_content = False

        if subgraphs == 'project':
            print('  subgraph cluster_%s {' % (project.ident))
            print('  color="#999999";')

        if include_networks or only_peered_vpc_projects:
            networks = project.get_in_vertices(
                'resource', type__eq='compute.googleapis.com/Network').all()
            ok_to_add = process_networks(graph, project, networks)

            if ok_to_add:
                project_had_content = True
                if include_routers != 'no':
                    routers = project.get_in_vertices(
                        'resource', type__eq='compute.googleapis.com/Router').all()
                    process_routers(graph, project, networks, routers)

        else:
            ok_to_add = True

        if ok_to_add or not only_peered_vpc_projects:
            if project.properties['data']['projectId'] in include_vms or 'all' in include_vms:
                instances = project.get_in_vertices(
                    'resource', type__eq='compute.googleapis.com/Instance').all()
                if process_instances(graph, project, instances):
                    project_had_content = True

            url = links['project'] % (urllib.parse.quote_plus(
                project.properties['data']['projectId']))

            if not args.no_empty_projects or project_had_content:
                processed_one_project = True
                print('  N_%s [shape=%s,label="%s",URL="%s",%s];' %
                      (project.ident, styles['shapes']['project'], format_label(project.properties['data']['name']), url, styles['project']))

                print('  N_%s -> N_%s [%s];' %
                      (project.ident, parent.ident, styles['edges']['project_to_folder']))

        if subgraphs == 'project':
            print('  }')

    return processed_one_project


def process_folders(graph, parent, folders):
    processed_one_project = False
    processed_one_folder = False
    for folder in folders:
        if len(include_folders) > 0:
            ok = False
            for f in include_folders:
                if f in folder.properties['ancestors'] or f == folder.properties['data']['name']:
                    ok = True
            if not ok:
                continue

        name = folder.properties['data']['name'].split('/')
        url = links['folder'] % (urllib.parse.quote_plus(
            name[-1]))

        sub_folders = folder.get_in_vertices(
            'resource', type__eq='cloudresourcemanager.googleapis.com/Folder').all()
        processed_folder = process_folders(graph, folder, sub_folders)
        if not processed_one_folder and processed_folder:
            processed_one_folder = True

        projects = folder.get_in_vertices(
            'resource', type__eq='cloudresourcemanager.googleapis.com/Project').all()
        processed_project = process_projects(graph, folder, projects)
        if not processed_one_project and processed_project:
            processed_one_project = True

        if not no_empty_folders or (processed_project or processed_folder):
            print('  N_%s [shape=%s,label="%s",URL="%s",%s];' %
                  (folder.ident, styles['shapes']['folder'], format_label(folder.properties['data']['displayName']), url, styles['folder']))

            print('  N_%s -> N_%s [%s];' %
                  (folder.ident, parent.ident, styles['edges']['folder_to_organization']))

    return processed_one_folder or processed_one_project


if args.mode == 'visualize':
    print('digraph GCP {')
    print('  overlap=%s;' % (styles['overlap']))
    print('  splines=%s;' % (styles['splines']))
    print('  graph[label="%s",%s];' % (args.title, styles['graph']))
    print('  node[%s];' % (styles['node']))
    print('  edge[%s];' % (styles['edge']))

    # Cache all networks
    org_networks = graph.get_vertices(
        'resource', type__eq='compute.googleapis.com/Network').all()
    for network in org_networks:
        all_networks.append(network.properties['data']['selfLink'])

    organizations = graph.get_vertices(
        'resource', type__eq='cloudresourcemanager.googleapis.com/Organization')

    if len(include_folders) > 0:
        for organization in organizations.all():
            all_folders = graph.get_vertices(
                'resource', type__eq='cloudresourcemanager.googleapis.com/Folder').all()
            for folder in only_folders:
                for f in all_folders:
                    if f.properties['data']['name'].split('/')[-1] == folder:
                        for ancestor in f.properties['ancestors']:
                            if ancestor.startswith('folders/') and ancestor not in only_folders:
                                only_folders.append(ancestor)

    process_organizations(graph, organizations.all())

    print('}')
