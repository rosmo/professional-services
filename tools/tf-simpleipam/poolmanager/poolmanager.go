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
package poolmanager

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
)

type PoolFile struct {
	IpPools         map[string][][]string `json:"pools"`
	AllocatedRanges map[string]string     `json:"allocated"`
}

type PoolManager struct {
	id              string
	poolFile        string
	poolBucket      string
	poolObject      string
	timeout         int
	poolLoaded      bool
	pool            PoolFile
	expandedRanges  map[string][][]string
	allocatedRanges map[string]string
	poolLock        *sync.Mutex
}

func NewPoolManager(id string, pool_file string, timeout int) *PoolManager {
	return &PoolManager{
		id:         id,
		poolFile:   pool_file,
		timeout:    timeout,
		poolLoaded: false,
		pool:       PoolFile{},
		poolLock:   &sync.Mutex{},
	}
}

func (pm *PoolManager) ipToUint32(ip net.IP) uint32 {
	if len(ip) == 16 {
		return binary.BigEndian.Uint32(ip[12:16])
	}
	return binary.BigEndian.Uint32(ip)
}

func (pm *PoolManager) uint32ToIp(ipInt uint32) net.IP {
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, ipInt)
	return ip
}

func (pm *PoolManager) savePoolFile() error {
	contents, err := json.MarshalIndent(pm.pool, "  ", "  ")
	if err != nil {
		return err
	}

	if strings.HasPrefix(pm.poolFile, "gs://") {
		ctx := context.Background()
		ctx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(pm.timeout))
		defer cancel()

		client, err := storage.NewClient(ctx)
		if err != nil {
			return err
		}

		log.Printf("Writing pool to bucket: %s, object: %s", pm.poolBucket, pm.poolObject)
		wc := client.Bucket(pm.poolBucket).Object(pm.poolObject).NewWriter(ctx)
		if _, err = wc.Write(contents); err != nil {
			return err
		}
		if err := wc.Close(); err != nil {
			return err
		}
	} else {
		err = ioutil.WriteFile(pm.poolFile, contents, 0640)
		if err != nil {
			return err
		}
	}
	return nil
}

func (pm *PoolManager) acquireLockFile() error {
	log.Printf("Acquiring internal pool lock.")
	pm.poolLock.Lock()
	lockOk := false
	defer func() {
		if !lockOk {
			log.Printf("Releasing internal pool lock due to error.")
			pm.poolLock.Unlock()
		}
	}()
	if !strings.HasPrefix(pm.poolFile, "gs://") {
		return nil
	}
	startTime := time.Now().Unix()
	for {
		ctx := context.Background()
		ctx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(pm.timeout))
		defer cancel()

		client, err := storage.NewClient(ctx)
		if err != nil {
			return err
		}

		lockFile := fmt.Sprintf("%s.lock", pm.poolObject)
		log.Printf("Trying to acquire lock file: %s", lockFile)
		o := client.Bucket(pm.poolBucket).Object(lockFile)
		wc := o.If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)

		if err := wc.Close(); err != nil {
			if !strings.Contains(err.Error(), "412") { // conditionNotMet
				return err
			}
			time.Sleep(2 * time.Second)
			nowTime := time.Now().Unix()
			if (nowTime - startTime) > int64(pm.timeout) {
				break
			}
		} else {
			// Lock file acquired
			lockOk = true
			return nil
		}
	}
	return fmt.Errorf("timeout while waiting for lockfile")
}

func (pm *PoolManager) releaseLockFile() error {
	defer func() {
		log.Printf("Releasing internal pool lock.")
		pm.poolLock.Unlock()
	}()
	if !strings.HasPrefix(pm.poolFile, "gs://") {
		return nil
	}
	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(pm.timeout))
	defer cancel()

	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}

	lockFile := fmt.Sprintf("%s.lock", pm.poolObject)
	log.Printf("Releasing a lock file: %s", lockFile)
	o := client.Bucket(pm.poolBucket).Object(lockFile)
	if err := o.Delete(ctx); err != nil {
		return err
	}

	return nil
}

func (pm *PoolManager) parsePoolFile() error {
	if strings.HasPrefix(pm.poolFile, "gs://") {
		url, err := url.Parse(pm.poolFile)
		if err != nil {
			return err
		}
		pm.poolBucket = url.Host
		pm.poolObject = url.Path[1:]
		log.Printf("Using Cloud Storage, bucket: %s, object: %s", pm.poolBucket, pm.poolObject)
	}
	return nil
}

func (pm *PoolManager) loadPoolFile() error {
	var contents []byte
	err := pm.parsePoolFile()
	if err != nil {
		return err
	}
	if pm.poolBucket != "" {
		ctx := context.Background()
		ctx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(pm.timeout))
		defer cancel()

		client, err := storage.NewClient(ctx)
		if err != nil {
			return err
		}

		rc, err := client.Bucket(pm.poolBucket).Object(pm.poolObject).NewReader(ctx)
		if err != nil {
			return err
		}
		defer rc.Close()

		contents, err = ioutil.ReadAll(rc)
		if err != nil {
			return err
		}
	} else {
		poolFile, err := os.Open(pm.poolFile)
		if err != nil {
			return err
		}
		defer poolFile.Close()

		log.Printf("Opened local file: %v", pm.poolFile)

		contents, err = ioutil.ReadAll(poolFile)
		if err != nil {
			return err
		}
	}

	err = json.Unmarshal(contents, &pm.pool)
	if err != nil {
		return err
	}

	// Reverse map to speed up lookup
	pm.allocatedRanges = make(map[string]string, len(pm.pool.AllocatedRanges))
	for k, v := range pm.pool.AllocatedRanges {
		pm.allocatedRanges[v] = k
	}

	pm.expandedRanges = make(map[string][][]string, len(pm.pool.IpPools))
	for k, v := range pm.pool.IpPools {
		pm.expandedRanges[k] = make([][]string, len(v))
		for idx, r := range v {
			var res []string
			for _, rr := range r {
				begin := strings.SplitN(rr, "-", 2)
				if len(begin) != 2 {
					return fmt.Errorf("not well-formatted range start: %s", rr)
				}
				end := strings.SplitN(begin[1], "/", 2)
				if len(end) != 2 {
					return fmt.Errorf("not well-formatted range end: %s", begin[1])
				}
				rangeStart := net.ParseIP(begin[0])
				rangeEnd := net.ParseIP(end[0])
				rangeStartInt := pm.ipToUint32(rangeStart)
				rangeEndInt := pm.ipToUint32(rangeEnd)
				currentRange := rangeStartInt
				maskFloat, err := strconv.ParseFloat(end[1], 64)
				if err != nil {
					return err
				}
				maskIncrease := uint32(math.Pow(2.0, 32.0-maskFloat))
				for {
					currentRange = currentRange + maskIncrease
					rangeStr := fmt.Sprintf("%s/%s", pm.uint32ToIp(currentRange).String(), end[1])
					// Remove ranges that are already allocated
					if _, ok := pm.allocatedRanges[rangeStr]; !ok {
						res = append(res, rangeStr)
					}

					if currentRange >= rangeEndInt {
						break
					}
				}
				log.Printf("Range %s expanded: %s to %s, mask %s", k, begin[0], end[0], end[1])
			}
			pm.expandedRanges[k][idx] = res
		}
	}
	pm.poolLoaded = true
	return nil
}

func (pm *PoolManager) GetAllocation(id string) (*string, error) {
	err := pm.parsePoolFile()
	if err != nil {
		return nil, err
	}
	if !pm.poolLoaded {
		err = pm.acquireLockFile()
		if err != nil {
			return nil, err
		}
		defer pm.releaseLockFile()

		err := pm.loadPoolFile()
		if err != nil {
			return nil, err
		}
	} else {
		pm.poolLock.Lock()
		defer pm.poolLock.Unlock()
	}
	if _, ok := pm.pool.AllocatedRanges[id]; ok {
		log.Printf("Found allocation: %s", id)
		allocatedRange := pm.pool.AllocatedRanges[id]
		return &allocatedRange, nil
	}
	return nil, fmt.Errorf("allocation not found: %s", id)
}

func (pm *PoolManager) DeleteAllocation(id string) error {
	err := pm.parsePoolFile()
	if err != nil {
		return err
	}
	err = pm.acquireLockFile()
	if err != nil {
		return err
	}
	defer pm.releaseLockFile()
	if !pm.poolLoaded {
		err := pm.loadPoolFile()
		if err != nil {
			return err
		}
	}
	log.Printf("Looking for allocation: %s", id)
	if _, ok := pm.pool.AllocatedRanges[id]; ok {
		log.Printf("Deleting allocation: %s", id)
		delete(pm.pool.AllocatedRanges, id)
		delete(pm.allocatedRanges, id)
		err := pm.savePoolFile()
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("allocation not found: %s", id)
}

func (pm *PoolManager) AllocateNewNetwork(poolName string, poolIndex int, maskSize int, name string) (*string, *string, error) {
	err := pm.parsePoolFile()
	if err != nil {
		return nil, nil, err
	}
	err = pm.acquireLockFile()
	if err != nil {
		return nil, nil, err
	}
	defer pm.releaseLockFile()

	if !pm.poolLoaded {
		err := pm.loadPoolFile()
		if err != nil {
			return nil, nil, err
		}
	}

	if _, ok := pm.expandedRanges[poolName]; !ok {
		return nil, nil, fmt.Errorf("unknown pool specified: %s", poolName)
	}
	if poolIndex == 0 || poolIndex > len(pm.expandedRanges[poolName]) {
		return nil, nil, fmt.Errorf("unknown pool index specified in \"%s\": %d (range 1..%d)", poolName, poolIndex, len(pm.expandedRanges[poolName]))
	}

	// Look for a suitable network to allocate
	var allocated string = ""
	maskQualifier := fmt.Sprintf("/%d", maskSize)
	for idx, rangeStr := range pm.expandedRanges[poolName][poolIndex-1] {
		if maskSize == 0 || strings.HasSuffix(rangeStr, maskQualifier) {
			copy(pm.expandedRanges[poolName][poolIndex-1][idx:], pm.expandedRanges[poolName][poolIndex-1][idx+1:])
			pm.expandedRanges[poolName][poolIndex-1][len(pm.expandedRanges[poolName][poolIndex-1])-1] = ""
			pm.expandedRanges[poolName][poolIndex-1] = pm.expandedRanges[poolName][poolIndex-1][:len(pm.expandedRanges[poolName][poolIndex-1])-1]
			allocated = rangeStr
			break
		}
	}

	if allocated != "" {
		log.Printf("Found range: %s", allocated)
		allocationName := fmt.Sprintf("%s-%s", pm.id, name)
		pm.allocatedRanges[allocationName] = allocated
		pm.pool.AllocatedRanges[allocationName] = allocated
		err := pm.savePoolFile()
		if err != nil {
			return nil, nil, err
		}
		return &allocated, &allocationName, nil
	}
	return nil, nil, fmt.Errorf("unable to find a suitable /%d network in pool: %s", maskSize, poolName)
}
