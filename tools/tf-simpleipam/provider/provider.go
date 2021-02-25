//   Copyright 2021 Google LLC
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
package provider

import (
	"github.com/GoogleCloudPlatform/professional-services/tools/simple-tf-ipam/poolmanager"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/terraform"
)

func Provider() terraform.ResourceProvider {
	return &schema.Provider{
		Schema: map[string]*schema.Schema{
			"id": {
				Type:        schema.TypeString,
				Description: "ID for this module (prevents name conflicts)",
				Required:    true,
			},
			"pool_file": {
				Type:        schema.TypeString,
				Description: "Location of pool file (local or gs://)",
				Required:    true,
			},
			"timeout": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     300,
				Description: "Timeout for waiting lock file",
				ForceNew:    true,
			},
		},
		ResourcesMap: map[string]*schema.Resource{
			"simpleipam_network": resourceItem(),
		},
		ConfigureFunc: providerConfigure,
	}
}

func providerConfigure(d *schema.ResourceData) (interface{}, error) {
	id := d.Get("id").(string)
	pool_file := d.Get("pool_file").(string)
	timeout := d.Get("timeout").(int)
	return poolmanager.NewPoolManager(id, pool_file, timeout), nil
}
