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
	"log"
	"strings"

	"github.com/GoogleCloudPlatform/professional-services/tools/simple-tf-ipam/poolmanager"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceItem() *schema.Resource {
	return &schema.Resource{
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Name of reservation",
				ForceNew:    true,
			},
			"pool": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Name of IP pool",
				ForceNew:    true,
			},
			"pool_index": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     1,
				Description: "IP pool index",
				ForceNew:    true,
			},
			"netmask": {
				Type:        schema.TypeInt,
				Optional:    true,
				Default:     0,
				Description: "Network mask size (use 0 or leave unset to grab any network from pool)",
				ForceNew:    true,
			},
			"network": {
				Type:        schema.TypeString,
				Description: "Reserved network range with mask",
				Computed:    true,
			},
			"network_no_mask": {
				Type:        schema.TypeString,
				Description: "Reserved network range without mask",
				Computed:    true,
			},
			"network_mask": {
				Type:        schema.TypeString,
				Description: "Network mask for reserved range",
				Computed:    true,
			},
		},
		Create: resourceCreateItem,
		Read:   resourceReadItem,
		Delete: resourceDeleteItem,
		Exists: resourceExistsItem,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
	}
}

func resourceCreateItem(d *schema.ResourceData, m interface{}) error {
	poolManager := m.(*poolmanager.PoolManager)

	name := d.Get("name").(string)
	poolName := d.Get("pool").(string)
	poolIndex := d.Get("pool_index").(int)
	netmask := d.Get("netmask").(int)

	log.Printf("Requesting new network range from %s (#%d) for %d: %s", poolName, poolIndex, netmask, name)
	allocatedNetwork, allocatedName, err := poolManager.AllocateNewNetwork(poolName, poolIndex, netmask, name)
	if err != nil {
		return err
	}
	log.Printf("Allocated network: %s, allocated name: %s", *allocatedName, *allocatedNetwork)
	networkStr := strings.SplitN(*allocatedNetwork, "/", 2)
	d.SetId(*allocatedName)
	d.Set("network", *allocatedNetwork)
	d.Set("network_no_mask", networkStr[0])
	d.Set("network_mask", networkStr[1])
	return resourceReadItem(d, m)
}

func resourceReadItem(d *schema.ResourceData, m interface{}) error {
	poolManager := m.(*poolmanager.PoolManager)

	allocId := d.Id()
	log.Printf("Reading allocation: %s\n", allocId)
	allocatedNetwork, err := poolManager.GetAllocation(allocId)
	if err != nil {
		return err
	}

	networkStr := strings.SplitN(*allocatedNetwork, "/", 2)
	d.Set("network", *allocatedNetwork)
	d.Set("network_no_mask", networkStr[0])
	d.Set("network_mask", networkStr[1])
	return nil
}

func resourceDeleteItem(d *schema.ResourceData, m interface{}) error {
	poolManager := m.(*poolmanager.PoolManager)

	allocId := d.Id()
	log.Printf("Deleting allocation: %s\n", allocId)
	err := poolManager.DeleteAllocation(allocId)
	if err != nil {
		return err
	}
	return nil
}

func resourceExistsItem(d *schema.ResourceData, m interface{}) (bool, error) {
	poolManager := m.(*poolmanager.PoolManager)

	allocId := d.Id()
	log.Printf("Reading allocation: %s\n", allocId)
	_, err := poolManager.GetAllocation(allocId)
	if err != nil {
		return false, nil
	}
	return true, nil
}
