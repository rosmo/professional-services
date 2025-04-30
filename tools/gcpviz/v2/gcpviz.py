#!/usr/bin/env python3
# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import argparse
from google.cloud import bigquery
import logging
import yaml
import textwrap
import coloredlogs
from jinja2 import Environment, TemplateError
import json
from typing import Any, Self
from py_d2 import D2Diagram, D2Shape, D2Connection, D2Style
from py_d2.shape import Shape
import os
import pprint

ResourceNode = tuple[str, str, str, dict]
D2Resources = tuple[dict[str, D2Shape], list[D2Connection]]
D2Shapes = list[D2Shape]
RelatedAsset = tuple[str, str]
Connection = tuple[str, str, str]

class Node:
    def __init__(self, resource: ResourceNode, children: list[Self], depth: int = 0, is_root: bool = False, is_container: bool = False):
        self.resource = resource
        self.children = children
        self.depth = depth
        self.is_root = is_root
        self.is_container = is_container

    def print(self):
        print(self)
        for v in self.children:
            v.print()

    def __str__(self):
        return "  "*self.depth + f"name={self.resource[0]}, children={len(self.children)}"

def get_all_asset_types(client: bigquery.Client, project_dataset: str,
                        cai_table: str) -> list[str]:
    logging.info("Fetching all asset types")
    asset_types_query = f"""
        SELECT DISTINCT asset_type AS asset_type FROM {project_dataset}.{cai_table}
    """
    results = client.query_and_wait(asset_types_query)
    asset_types = []
    for row in results:
        asset_type = row.get("asset_type")
        asset_types.append(asset_type)
    return asset_types


def generate_asset_views(client: bigquery.Client, project_dataset: str,
                         cai_table: str,
                         asset_types: list[str]) -> dict[str, str]:
    logging.info("Generating views for all assets")

    asset_tables = {}
    for asset_type in asset_types:
        asset_table_name = asset_type.replace(".googleapis.com", "").replace(
            ".", "_").replace("/", "_")

        view_table_name = f"{project_dataset}.asset_{asset_table_name}"
        view_query = f"""
            SELECT 
              * 
            FROM {project_dataset}.{cai_table} 
            WHERE 
                asset_type = '{asset_type}'
        """.strip()
        view_query = textwrap.dedent(view_query)

        try:
            asset_view = client.get_table(view_table_name)
            if asset_view.view_query != view_query:
                asset_view.view_query = view_query
                logging.info(f"Updating asset view: {view_table_name}")
                client.update_table(asset_view, ["view_query"])
        except Exception as e:
            asset_view = bigquery.Table(view_table_name)
            asset_view.view_query = view_query
            logging.info(f"Creating asset view: {view_table_name}")
            client.create_table(asset_view)

        asset_tables[asset_type] = f"asset_{asset_table_name}"
    return asset_tables


def get_relationship_asset_types(client: bigquery.Client, project_dataset: str,
                                 relationship_table: str) -> dict[str, str]:

    logging.info("Fetching all relationship asset types")
    asset_types_query = f"""
        SELECT DISTINCT asset_type, related_asset.asset_type AS related_asset_type FROM {project_dataset}.{relationship_table}
    """
    results = client.query_and_wait(asset_types_query)
    asset_types = {}
    for row in results:
        asset_type = row.get("asset_type")
        related_asset_type = row.get("related_asset_type")
        asset_types[related_asset_type] = asset_type
    return asset_types


def generate_relationship_views(client: bigquery.Client, project_dataset: str,
                                relationship_table: str,
                                asset_types: dict[str, str]) -> dict[str, str]:
    logging.info(
        f"Generating all {len(asset_types.keys())} relationship asset types")

    generated_views = {}
    for asset_type, related_asset_type in asset_types.items():
        asset = asset_type_to_table_name(asset_type)
        related_asset = asset_type_to_table_name(related_asset_type)
        view_table_name = f"{project_dataset}.graph_{asset}_to_{related_asset}"

        view_query = f"""
            SELECT 
                name AS name, related_asset.asset AS parent 
            FROM {project_dataset}.{relationship_table} 
            WHERE 
                asset_type = '{asset_type}' AND related_asset.asset_type = '{related_asset_type}'
        """.strip()
        view_query = textwrap.dedent(view_query)

        try:
            asset_view = client.get_table(view_table_name)
            if asset_view.view_query != view_query:
                asset_view.view_query = view_query
                logging.info(f"Updating relationship view: {view_table_name}")
                client.update_table(asset_view, ["view_query"])
        except Exception as e:
            asset_view = bigquery.Table(view_table_name)
            asset_view.view_query = view_query
            logging.info(f"Creating relationship view: {view_table_name}")
            client.create_table(asset_view)

        generated_views[
            f"{asset_type}:{related_asset_type}"] = (f"{asset}_to_{related_asset}", "Owns", "cai_relationship", None)
    return generated_views


def generate_additional_views(client: bigquery.Client, project_dataset: str,
                              cai_table: str,
                              additional_views: list[dict[str, str]]) -> None:
    generated_views = {}
    for additional_view in additional_views:
        additional_props = {"dynamic_label": False}
        # Synthesize additional asset tables
        if "assetTable" in additional_view:
            view_table_name = f"{project_dataset}.asset_{additional_view['assetTable']}"
            view_query = additional_view["assetQuery"].replace("{caiTable}",
                                  f"{project_dataset}.{cai_table}").strip()
            view_query = textwrap.dedent(view_query)
            try:
                asset_view = client.get_table(view_table_name)
                if asset_view.view_query != view_query:
                    asset_view.view_query = view_query
                    logging.info(f"Updating additional asset view: {view_table_name}")
                    client.update_table(asset_view, ["view_query"])
            except Exception as e:
                asset_view = bigquery.Table(view_table_name)
                asset_view.view_query = view_query
                logging.info(f"Creating additional asset view: {view_table_name}")
                client.create_table(asset_view)
            
            if "assetTableDynamicLabel" in additional_view and additional_view["assetTableDynamicLabel"]:
                additional_props["dynamic_label"] = True

        view_table_name = f"{project_dataset}.graph_{additional_view['table']}"

        source_table = f"{project_dataset}.asset_{additional_view['sourceTable']}"
        view_query = additional_view["query"].replace(
            "{sourceTable}",
            source_table).replace("{caiTable}",
                                  f"{project_dataset}.{cai_table}").strip()

        try:
            asset_view = client.get_table(view_table_name)
            if asset_view.view_query != view_query:
                asset_view.view_query = view_query
                logging.info(f"Updating relationship view: {view_table_name}")
                client.update_table(asset_view, ["view_query"])
        except Exception as e:
            asset_view = bigquery.Table(view_table_name)
            asset_view.view_query = view_query
            logging.info(f"Creating relationship view: {view_table_name}")
            client.create_table(asset_view)

        generated_views[additional_view["asset"]] = (additional_view["table"], "Owns", "additional", additional_props)
    return generated_views

def generate_custom_views(client: bigquery.Client, project_dataset: str, cai_table: str, all_asset_type: list[str], relationship_views: list[dict[str, str]], custom_relationships: dict[str, Any]) -> dict[str, str]:

    generated_views = {}
    for src_asset_type, fields in custom_relationships.items():
        if src_asset_type not in all_asset_types:
           logging.info(f"Skipping asset type due to no assets: {src_asset_type}")
           continue

        for field in fields:
            refs = []
            if "refs" in field:
                refs = field["refs"]
            else:
                refs = [field["ref"]]
            field_name = field["field"]

            if not field_name.startswith("resource.data."):
                logging.warning(f"Non resource.data fields not supported, skipping: {field_name}")
                continue

            filtered_refs = []
            for ref in refs:
                if ref not in all_asset_types:
                   logging.info(f"Skipping target asset type due to no assets: {ref}")
                else:
                    ref_id = f"{src_asset_type}:{ref}"
                    if ref_id in relationship_views.keys():
                        logging.info(f"Skipping target asset type due to existing relationship: {ref}")
                    else:
                        filtered_refs.append(ref)

            field_name_no_prefix = field_name[len("resource.data."):]

            # Most of the references are prefixed with this, so just replace it from all of them
            pre_fixed_rewrite = "REGEXP_REPLACE("
            post_fixed_rewrite = ", \"^https://www.googleapis.com/(.+)/v1/\", \"//\\\\1.googleapis.com/\")"

            pre_rewrite = ""
            post_rewrite = ""
            if "rewrite" in field:
                if "replace" in field["rewrite"]:
                    replace_config = field["rewrite"]["replace"]
                    pre_rewrite = f"REPLACE("
                    post_rewrite = f", \"{replace_config['from']}\", \"{replace_config['to']}\")"
                if "regexp" in field["rewrite"]:
                    regexp_config = field["rewrite"]["regexp"]
                    pre_rewrite = f"REGEXP_REPLACE("
                    post_rewrite = f", \"{regexp_config['from']}\", \"{regexp_config['to']}\")"

            for ref in filtered_refs:
                view_query = None
                src_table_name = asset_type_to_table_name(src_asset_type)
                dst_table_name = asset_type_to_table_name(ref)

                view_table_name = f"{project_dataset}.graph_{src_table_name}_to_{dst_table_name}"

                # Handle special cases where some JSON value needs to be extracted from an array
                if "[*]" in field_name:
                    field_array_split = field_name_no_prefix.split("[*]")
                    if len(field_array_split) == 2:
                        if field_array_split[1] != "":
                            view_query = f"""
                                SELECT 
                                    name, 
                                    {pre_rewrite}{pre_fixed_rewrite}JSON_VALUE(related_asset, "${field_array_split[1]}"){post_fixed_rewrite}{post_rewrite} AS parent
                                FROM
                                    {project_dataset}.{cai_table}
                                JOIN
                                    UNNEST(JSON_QUERY_ARRAY(resource.data, "$.{field_array_split[0]}")) AS related_asset
                                WHERE 
                                    asset_type = '{src_asset_type}'
                            """
                        else:
                            view_query = f"""
                                SELECT 
                                    name, 
                                    {pre_rewrite}{pre_fixed_rewrite}JSON_VALUE(related_asset, "$"){post_fixed_rewrite}{post_rewrite} AS parent
                                FROM
                                    {project_dataset}.{cai_table}
                                JOIN
                                    UNNEST(JSON_QUERY_ARRAY(resource.data, "$.{field_array_split[0]}")) AS related_asset
                                WHERE 
                                    asset_type = '{src_asset_type}'
                            """
                    else:
                        logging.fatal(f"Unsupported field split: {field_name}")
                else:
                    view_query = f"""
                        SELECT 
                            name, 
                            {pre_rewrite}{pre_fixed_rewrite}JSON_VALUE(resource.data, "$.{field_name_no_prefix}"){post_fixed_rewrite}{post_rewrite} AS parent
                        FROM 
                            {project_dataset}.{cai_table} 
                        WHERE 
                            asset_type = '{src_asset_type}'
                    """

                view_query = textwrap.dedent(view_query)
                try:
                    asset_view = client.get_table(view_table_name)
                    if asset_view.view_query != view_query:
                        asset_view.view_query = view_query

                        logging.info(f"Updating custom relationship view: {view_table_name}")
                        logging.debug(f"View query: {view_query}")
                        client.update_table(asset_view, ["view_query"])
                except Exception as e:
                    asset_view = bigquery.Table(view_table_name)
                    asset_view.view_query = view_query
                    logging.info(f"Creating custom relationship view: {view_table_name}")
                    logging.debug(f"View query: {view_query}")
                    client.create_table(asset_view)

                ref_id = f"{src_asset_type}:{ref}"
                generated_views[ref_id] = (f"{src_table_name}_to_{dst_table_name}", "Relates", "custom", None)

    return generated_views

def generate_graph(client: bigquery.Client, dataset: str, graph_name: str,
                   asset_types: dict[str, str]) -> None:

    points_to_resources = ["cai_relationship", "custom"]

    node_tables_list = []
    node_tables_visited = []
    all_node_labels = []
    all_edge_labels = []
    for asset_type, asset_info in asset_types.items():
        asset_table, asset_label, relationship_type, additional_properties = asset_info
        if relationship_type in points_to_resources:
            continue

        asset_type_list = asset_type.split(":")
        source_table = asset_type_to_table_name(asset_type_list[0])

        if source_table not in node_tables_visited:
            label_list = source_table.replace("_", " ").split(" ")
            label_list_cap = [
                label[0].upper() + label[1:] for label in label_list
            ]
            label = "".join(label_list_cap)

            all_node_labels.append(label)
            label_config = f"LABEL {label}"
            if additional_properties:
                if "dynamic_label" in additional_properties and additional_properties["dynamic_label"]:
                    label_config = "DYNAMIC LABEL (label)"

            node_table = f"""
                    {dataset}.asset_{source_table} AS asset_{source_table}
                        KEY (name)
                        {label_config}
                        PROPERTIES (name, resource.parent AS parent, asset_type AS asset_type)
            """.rstrip()
            node_tables_list.append(node_table)
            node_tables_visited.append(source_table)

        # No specific source on the right side, continue
        if asset_type_list[1] == "":
            continue

        source_table = asset_type_to_table_name(asset_type_list[1])
        if source_table in node_tables_visited:
            continue

        label_list = source_table.replace("_", " ").split(" ")
        label_list_cap = [label[0].upper() + label[1:] for label in label_list]
        label = "".join(label_list_cap)

        node_table = f"""
                {dataset}.asset_{source_table} AS asset_{source_table}
                    KEY (name)
                    LABEL {label}
                    PROPERTIES (name, resource.parent AS parent, asset_type AS asset_type)
        """.rstrip()
        node_tables_list.append(node_table)
        node_tables_visited.append(source_table)

    edge_tables_list = []
    for asset_type, asset_info in asset_types.items():

        asset_table, asset_label, relationship_type, additional_properties = asset_info

        asset_type_list = asset_type.split(":")
        # from_asset = asset_type_to_table_name(asset_type_list[0])
        # to_asset = asset_type_to_table_name(asset_type_list[1])

        # label_list = f"{from_asset}_to_{to_asset}".replace("_", " ").split(" ")
        # label_list_cap = [label.capitalize() for label in label_list]
        # label = "".join(label_list_cap)
        label = asset_label

        asset_type_list = asset_type.split(":")
        source_table = asset_type_to_table_name(asset_type_list[0])
        target_table = asset_type_to_table_name(asset_type_list[1])

        full_source_table = f"asset_{source_table}"
        full_target_table = f"asset_{target_table}"

        if relationship_type in points_to_resources:
            full_source_table = "asset_Resources"
            full_target_table = "asset_Resources"

        label_config = f"LABEL {label}"

        all_edge_labels.append(label)
        edge_table = f"""
                {dataset}.graph_{asset_table}
                    KEY (name, parent)
                    SOURCE KEY (parent) REFERENCES {full_target_table} (name)
                    DESTINATION KEY (name) REFERENCES {full_source_table} (name)
                    {label_config}
        """.rstrip()
        edge_tables_list.append(edge_table)

    node_table_str = ",\n".join(node_tables_list)
    edge_table_str = ",\n".join(edge_tables_list)

    graph_create = f"""
        CREATE OR REPLACE PROPERTY GRAPH {dataset}.{graph_name}
            NODE TABLES (
                {node_table_str}
            )
            EDGE TABLES (
                {edge_table_str}
            )
    """.strip()
    logging.info(f"Creating or replacing asset graph: {dataset}.{graph_name}")
    graph_create_deindented = textwrap.dedent(graph_create)
    logging.debug(f"Graph definition:\n{graph_create_deindented}")
    create_job = client.query(graph_create)
    create_job.result()
    logging.info(f"All available node labels: {', '.join(all_node_labels)}")
    logging.info(f"All available edge labels: {', '.join(all_edge_labels)}")


def asset_type_to_table_name(asset_type: str) -> str:
    asset_type_split_dot = asset_type.split(".")

    if len(asset_type_split_dot) == 1:
        return asset_type

    asset_type_split_slash = asset_type.split("/")

    if asset_type_split_dot[1] == "googleapis":
        asset_api = asset_type_split_dot[0]
    else:
        asset_api = asset_type_split_slash[0].replace(".", "_")
    asset_name = asset_type_split_slash[1]
    return f"{asset_api}_{asset_name}"

def graph_shape_name(resource: str) -> str:
    return resource.replace(".googleapis.com", "").lstrip("/").replace(".", "-").replace("/", "-").replace("-", "-").replace("_", "-").replace("@", "").replace("--", "-").lower()

def render_shape_template(resource: ResourceNode, key: str, default_value: str, shape_template: dict[str, Any], none_on_empty: bool = False) -> str:
    
    template = default_value
    if "default" in shape_template and key in shape_template["default"]:
        template = shape_template["default"][key]
    if resource[2] in shape_template and key in shape_template[resource[2]]:
        template = shape_template[resource[2]][key]

    template = jinja_env.from_string(template)
    template.name = "key"
    result = template.render({"name": resource[0], "asset_type": resource[2], "resource": resource[3]})
    if none_on_empty and result == "":
        return None
    return result


def render_node_shape(jinja_env: Environment, node: Node, shape_template: dict[str, Any]) -> D2Shape:
    resource = node.resource

    label = render_shape_template(resource, "label", "{{ resource.name|default(name)|basename }}", shape_template)
    icon = render_shape_template(resource, "icon", "", shape_template, True)

    shape = D2Shape(name=graph_shape_name(resource[0]))
    if resource[2] in shape_template and "markdown" in shape_template[resource[2]] and shape_template[resource[2]]["markdown"]:
        shape.label = "|`md\n" + label.strip() + "\n`|"
    else:
        shape.label = label.strip().replace("\n", "\\n")
    shape.icon = icon
    
    style = {
        "stroke": "stroke",
        "stroke_width": "strokeWidth",
        "fill": "fill",
        "shadow": "shadow",
        "opacity": "opacity",
        "stroke_dash": "strokeDash",
        "three_d": "threeD"
    }
    node_style = {}
    if "default" in shape_template and "style" in shape_template["default"]:
        node_style = shape_template["default"]["style"]
    if resource[2] in shape_template and "style" in shape_template[resource[2]]:
        node_style = {
            **node_style,
            **shape_template[resource[2]]["style"]
        }
    final_style = {}
    if len(node_style.keys()) > 0:
        for k, v in style.items():
            if v in node_style:
                template = jinja_env.from_string(str(node_style[v]))
                template.name = "style"
                value = template.render({"name": resource[0], "asset_type": resource[2], "resource": resource[3]})
                if value != "":
                    final_style[k] = value
    if len(final_style.keys()) > 0:
        d2_style = D2Style(**final_style)
        shape.style = d2_style
        
    return shape

def render_node(jinja_env: Environment, node: Node, shape_template: dict[str, Any]) -> D2Shapes:

    child_shapes = []
    for node_child in node.children:
        child_shapes = child_shapes + render_node(jinja_env, node_child, shape_template)

    resource = node.resource
    label = resource[0]
    shape = render_node_shape(jinja_env, node, shape_template)
    shape.shapes = child_shapes
    return [shape]
            
def tree_walker(parent: str, resources: dict[str, list[ResourceNode]], node_settings: dict[str, dict[str, Any]], depth: int = 0, parent_path: list[str] = []) -> tuple[list[Node], list[Connection], dict[str, list[str]]]:
    nodes = []
    connections = []
    connection_paths = {}
    if parent in resources:
        for resource in resources[parent]:
            is_container = False
            if resource[2] in node_settings and node_settings[resource[2]]["is_container"]:
                is_container = True
                new_parent_path = parent_path + [resource[0]]
                child_nodes, child_connections, child_paths = tree_walker(resource[0], resources, node_settings, depth + 1, new_parent_path)

                # Filter out child connections that are connected to the parent, since it's already a container
                # there is no need
                new_child_connections = []
                for child_connection in child_connections:
                    if child_connection[0] != resource[0]:
                        new_child_connections.append(child_connection)
                child_connections = new_child_connections

            else:
                child_nodes, child_connections, child_paths = tree_walker(resource[0], resources, node_settings, depth + 1, parent_path)

            connection_paths[resource[0]] = parent_path
            connection_paths = { 
                **child_paths,
                **connection_paths 
            }

            is_root = (depth == 0)
            if is_container:
                node = Node(resource, children=child_nodes, depth=depth, is_root=is_root, is_container=True)
                nodes.append(node)
            else:
                node = Node(resource, children=[], depth=depth, is_root=is_root, is_container=False)
                nodes.append(node)
                for child_node in child_nodes:
                    nodes.append(child_node)

            # Root nodes 
            if depth > 0:
                connections.append((parent, resource[0]))

            # Connect related assets
            for related_asset in resource[4]:
                if related_asset[0] != parent:
                    parent_shape_name = graph_shape_name(resource[0])
                    node_shape_name = graph_shape_name(related_asset[0])
                    connections.append((resource[0], related_asset[0]))

            connections = connections + child_connections
                        
    return (nodes, connections, connection_paths)

def dump_walker(parent: str, resources: dict[str, list[ResourceNode]], depth: int = 0) -> None:
    depth_str = " "*(depth*4)
    if parent in resources:
        for resource in resources[parent]:
            logging.debug(f"{depth_str}{resource[0]}")
            dump_walker(resource[0], resources, depth + 1)

def graph_query(client: bigquery.Client, query: str, fields: dict[str, str], node_settings: dict[str, dict[str, Any]], jinja_env: Environment) -> tuple[list[Node], list[Connection]]:
    logging.info(f"Running query: {query}")
    
    results = client.query_and_wait(query)
    resources = {}
    resource_names = []
    
    for row in results:
        resource = row.get(fields["resource"])
        if not resource:
            logging.fatal(f"Missing resource ({fields['resource']}) field in {row}!")
            return
        resource_name = row.get(fields["name"])
        resource_type = row.get(fields["type"])

        resource_data = resource[fields["resourceData"]]    
        parent = resource[fields["parent"]]
        if not parent:
            logging.warning(f"Ignoring non-parented resource: {resource_name}")
            continue
        if parent not in resources:
            resources[parent] = []

        related_assets = []
        if row.get(fields["relatedAssets"]):
            for related_asset in row.get(fields["relatedAssets"]):
                related_asset_name = related_asset[fields["relatedAssetsName"]]
                related_asset_type = related_asset[fields["relatedAssetsType"]]
                if related_asset_name != resource_name:
                    related_assets.append((related_asset_name, related_asset_type))

        resources[parent].append((resource_name, parent, resource_type, json.loads(resource_data), related_assets))
        resource_names.append(resource_name)
    
    # Filter related assets that don't exist
    for parent in resources.keys():
        for idx, resource in enumerate(resources[parent]):
            related_assets = resource[4]
            resource[4][:] = [i for i in resource[4] if i[0] in resource_names]

    logging.info(f"Retrieved {len(resource_names)} resources.")

    new_resources = {}
    for parent, parent_resources in resources.items():
        new_resources[parent] = []
        resources_to_remove = []
        for parent_resource in parent_resources:
            resource_name, resource_parent, resource_type, resource_data, related_assets = parent_resource
            reparented = False
            if len(related_assets) > 0:
                for related_asset in related_assets:
                    if node_settings[related_asset[1]]["connect_related"]:
                        new_resource_parent = related_asset[0]
                        if new_resource_parent not in new_resources:
                            new_resources[new_resource_parent] = []
                        
                        if new_resource_parent == parent:
                            logging.warning(f"Already reparented resource: {related_asset}")
                            continue
                        
                        new_resources[new_resource_parent].append(parent_resource)
                        reparented = True
            if not reparented:
                new_resources[parent].append(parent_resource)
    
    resources = new_resources
    
    # Find root level
    root_parents = []
    for parent in resources.keys():
        if parent not in resource_names:
            root_parents.append(parent)  

    logging.debug("Printing the resource tree:")
    for parent in root_parents:
        dump_walker(parent, resources)

    nodes = []
    connections = []
    paths = {}
    for parent in root_parents:
        sub_nodes, sub_connections, sub_paths = tree_walker(parent, resources, node_settings)
        nodes = nodes + sub_nodes
        connections = connections + sub_connections
        paths = {
            **paths,
            **sub_paths
        }

    return nodes, connections, paths

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("-dataset", help="Dataset name", required=True)
    parser.add_argument("-query-file",
                        help="Query source file")
    parser.add_argument("-diagram-file",
                        default="graph.d2",
                        help="Result D2 diagram file")
    parser.add_argument("--query-parameters", help="JSON string to parse and pass as parameters to the query")
    parser.add_argument("-cai-table", default="cai", help="Asset table")
    parser.add_argument("-relationship-table",
                        default="relationships",
                        help="Relationships table")
    parser.add_argument("-graph-name",
                        default="asset_graph",
                        help="Asset graph name")
    parser.add_argument("-project", help="Project ID")
    parser.add_argument("-location", help="Dataset location")
    parser.add_argument("-config-file",
                        default="config.yaml",
                        help="Configuration data file")
    parser.add_argument("--skip-graph-create",
                        help="Skip graph creation",
                        action="store_true")
    parser.add_argument("--log-level", default="INFO", help="Set log level")

    args = parser.parse_args()

    logger = logging.getLogger("gcpviz")
    coloredlogs.install(level=args.log_level.upper())

    logging.info("gcpviz v2 by Google Professional Services")

    config = {}
    logging.info(f"Reading configuration from: {args.config_file}")
    with open(args.config_file) as f:
        config = yaml.load(f, Loader=yaml.SafeLoader)

    client = bigquery.Client(project=args.project, location=args.location)

    project_dataset = args.dataset
    if args.project:
        project_dataset = f"{args.project}.{args.dataset}"

    all_asset_types = get_all_asset_types(client, project_dataset, args.cai_table)
    if not args.skip_graph_create:

        # We no longer required dynamic asset tables as we use dynamic labels
        #asset_type_tables = generate_asset_views(client, project_dataset,
        #                                         args.cai_table,
        #                                         all_asset_types)

        asset_types = get_relationship_asset_types(client, project_dataset,
                                                   args.relationship_table)
        relationship_views = generate_relationship_views(
            client, project_dataset, args.relationship_table, asset_types)

        if "additionalViews" in config:
            additional_relationship_views = generate_additional_views(
                client, project_dataset, args.cai_table,
                config["additionalViews"])
            relationship_views = {
                **relationship_views,
                **additional_relationship_views
            }

        if "customRelationships" in config:
            custom_relationship_views = generate_custom_views(client, project_dataset, args.cai_table, all_asset_types, relationship_views, config["customRelationships"])
            relationship_views = {
                **relationship_views,
                **custom_relationship_views
            }

        generate_graph(client, args.dataset, args.graph_name,
                       relationship_views)
        
    if args.query_file:
        logging.info(f"Loading query from: {args.query_file}")
        query = None
        with open(args.query_file) as f:
            query = yaml.load(f, Loader=yaml.SafeLoader)
        if "query" not in query:
            logging.fatal("No query specified in the query file!")

        query_to_run = query["query"]
        jinja_env = Environment(autoescape=lambda _: False, cache_size=0,
                      extensions=['jinja2.ext.do'])
        jinja_env.globals["graph"] = f"{args.dataset}.{args.graph_name}"
        jinja_env.globals["assets"] = f"{args.dataset}.{args.cai_table}"
        query_parameters = {}
        if args.query_parameters:
            logging.info(f"Query parameters: {args.query_parameters}")
            query_parameters = json.loads(args.query_parameters)
            jinja_env.globals = {
                **jinja_env.globals,
                **query_parameters
            }
        
        jinja_env.filters.update({
            "basename": lambda s: os.path.basename(s)
        })

        fields = {
            "relatedAssets": "related_assets",
            "relatedAssetsName": "related_asset",
            "relatedAssetsType": "related_asset_type",
            "name": "name",
            "type": "asset_type",
            "resource": "resource",
            "parent": "parent",
            "resourceData": "data",
        }
        if "fields" in query:
            for field_name, ref_value in query["fields"].items():
                fields[field_name] = ref_value

        asset_type_filter = "IS NOT NULL"
        if "filter" in query:
            filter_exclude = query["filter"]["exclude"] if "filter" in query and "exclude" in query["filter"] else []
            filter_include = query["filter"]["include"] if "filter" in query and "include" in query["filter"] else []
            if len(filter_include) > 0:
                filters = map(lambda f: f"'{f}'", filter_include)
                asset_type_filter = f"IN ({','.join(filters)})"
            elif len(filter_exclude) > 0:
                filters = map(lambda f: f"'{f}'", filter_exclude)
                asset_type_filter = f"NOT IN ({','.join(filters)})"

        jinja_env.globals["asset_type_filter"] = asset_type_filter

        query_template = jinja_env.from_string(query_to_run)
        query_template.name = "cql"
        query_to_run = query_template.render()

        node_settings = {}
        for asset_type in all_asset_types:
            if asset_type not in node_settings:
                    node_settings[asset_type] = {
                        "is_container": False,
                        "connect_related": False
                    }
                
        if "containerResources" in query:
            for container in query["containerResources"]:
                node_settings[container] = {
                    **node_settings[container],
                    **{
                         "is_container": True
                    }
                }
        if "connectRelatedAssets" in query:
            for container in query["connectRelatedAssets"]:
                node_settings[container] = {
                    **node_settings[container],
                    **{
                         "connect_related": True
                    }
                }

        nodes, connections, paths = graph_query(client, query_to_run, fields, node_settings, jinja_env)

        shape_template = {}
        if "shapes" in query:
            shape_template = query["shapes"]

        shapes = []
        total_shapes = 0
        for node in nodes:
            shapes = shapes + render_node(jinja_env, node, shape_template)
        
        for shape in shapes:
            total_shapes += 1 + len(shape.shapes)

        shape_connections = []
        connection_pairs = []
        for connection in connections:
            left_connection_path = paths[connection[0]]
            left_connection_path = list(map(lambda s: graph_shape_name(s), left_connection_path))
            left_connection_final_path = ".".join(left_connection_path) + ("." if len(left_connection_path) > 0 else "") + graph_shape_name(connection[0])

            right_connection_path = paths[connection[1]]
            right_connection_path = list(map(lambda s: graph_shape_name(s), right_connection_path))
            right_connection_final_path = ".".join(right_connection_path) + ("." if len(right_connection_path) > 0 else "") + graph_shape_name(connection[1])
            
            path_key = f"{left_connection_final_path}:{right_connection_final_path}"
            if path_key not in connection_pairs:
                shape_connections.append(D2Connection(shape_1=left_connection_final_path, shape_2=right_connection_final_path))
                connection_pairs.append(path_key)

        logging.info(f"Processed {total_shapes} shapes.")
        
        pre = ""
        post = ""
        if "diagram" in query and "pre" in query["diagram"]:
            pre = query["diagram"]["pre"] + "\n"
        if "diagram" in query and "post" in query["diagram"]:
            post = "\n" + query["diagram"]["post"]

        diagram = D2Diagram(shapes=shapes, connections=shape_connections)
        if args.diagram_file and args.diagram_file != "":
            logging.info(f"Writing diagram to file: {args.diagram_file}")
            with open(args.diagram_file, "w", encoding="utf-8") as f:
                f.write(pre)
                f.write(str(diagram))
                f.write(post)
        else:
            print(pre + str(diagram) + post)
    else:
        logging.info("Finished, no query specified or executed.")
