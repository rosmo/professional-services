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

import yaml
import argparse
import sys
import os
from deepmerge import Merger

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("-query-file",
                        help="Query source file")
    parser.add_argument("-query-output-file",
                        help="Query output file")
    args = parser.parse_args()

    with open(args.query_file) as f:
        full_query = yaml.load(f, Loader=yaml.SafeLoader)
    
    partial_query_str = os.getenv("GCPVIZ_PARTIAL_QUERY")
    if not partial_query_str:
        raise Exception("GCPVIZ_PARTIAL_QUERY is not set!")
    partial_query = yaml.load(partial_query_str, Loader=yaml.SafeLoader)

    merger = Merger(
        [
            (list, ["override"]),
            (dict, ["merge"]),
            (set, ["override"])
        ],
        ["override"],
        ["override"]
    )
    merger.merge(full_query, partial_query)
    with open(args.query_output_file, "w") as f: 
        f.write(yaml.dump(full_query))