package main

import (
	"encoding/json"
	"log"
	"strings"
)

func MakeMonQuery(cephconn *Cephconnection, query map[string]string) []byte {
	monjson, err := json.Marshal(query)
	if err != nil {
		log.Fatalf("Can't marshal json mon query. Error: %v", err)
	}

	monrawanswer, _, err := cephconn.conn.MonCommand(monjson)
	if err != nil {
		log.Fatalf("Failed exec monCommand. Error: %v", err)
	}
	return monrawanswer
}

func GetPoolSize(cephconn *Cephconnection, params Params) Poolinfo {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "osd pool get", "pool": params.pool,
		"format": "json", "var": "size"})
	monanswer := Poolinfo{}
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer

}

func GetPgByPool(cephconn *Cephconnection, params Params) []PlacementGroup {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "pg ls-by-pool", "poolstr": params.pool,
		"format": "json"})
	var monanswer []PlacementGroup
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer
}

func GetOsdCrushDump(cephconn *Cephconnection) OsdCrushDump {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "osd crush dump", "format": "json"})
	var monanswer OsdCrushDump
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer
}

func GetOsdDump(cephconn *Cephconnection) OsdDump {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "osd dump", "format": "json"})
	var monanswer OsdDump
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer
}

func GetOsdMetadata(cephconn *Cephconnection) []OsdMetadata {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "osd metadata", "format": "json"})
	var monanswer []OsdMetadata
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer
}

func GetObjActingPrimary(cephconn *Cephconnection, params Params, objname string) int64 {
	monrawanswer := MakeMonQuery(cephconn, map[string]string{"prefix": "osd map", "pool": params.pool,
		"object": objname, "format": "json"})
	var monanswer OsdMap
	if err := json.Unmarshal([]byte(monrawanswer), &monanswer); err != nil {
		log.Fatalf("Can't parse monitor answer. Error: %v", err)
	}
	return monanswer.UpPrimary
}

func GetCrushHostBuckets(buckets []Bucket, itemid int64) []Bucket {
	var rootbuckets []Bucket
	for _, bucket := range buckets {
		if bucket.ID == itemid {
			if bucket.TypeName == "host" {
				rootbuckets = append(rootbuckets, bucket)
			} else {
				for _, item := range bucket.Items {
					result := GetCrushHostBuckets(buckets, item.ID)
					for _, it := range result {
						rootbuckets = append(rootbuckets, it)
					}
				}
			}
		}
	}
	return rootbuckets
}

func GetOsdForLocations(params Params, osdcrushdump OsdCrushDump, osddump OsdDump, poolinfo Poolinfo, osdsmetadata []OsdMetadata) []Device {
	var crushrule, rootid int64
	var crushrulename string
	for _, pool := range osddump.Pools {
		if pool.Pool == poolinfo.PoolId {
			crushrule = pool.CrushRule
		}
	}
	for _, rule := range osdcrushdump.Rules {
		if rule.RuleID == crushrule {
			crushrulename = rule.RuleName
			for _, step := range rule.Steps {
				if step.Op == "take" {
					rootid = step.Item
				}
			}
		}
	}

	osddevices := []Device{}
	bucketitems := GetCrushHostBuckets(osdcrushdump.Buckets, rootid)
	if params.define != "" {
		if strings.HasPrefix(params.define, "osd.") {
			for _, hostbucket := range bucketitems {
				for _, item := range hostbucket.Items {
					for _, device := range osdcrushdump.Devices {
						if device.ID == item.ID && params.define == device.Name {
							for _, osdmetadata := range osdsmetadata {
								if osdmetadata.ID == device.ID {
									device.Info = osdmetadata
									osddevices = append(osddevices, device)
								}

							}
						}
					}
				}
			}
			if len(osddevices) == 0 {
				log.Fatalf("Defined osd not exist in root for rule: %v pool: %v.\nYou should define osd like osd.X",
					crushrulename, poolinfo.Pool)
			}
		} else {
			for _, hostbucket := range bucketitems {
				if strings.Split(hostbucket.Name, "~")[0] == strings.Split(params.define, "~")[0] { //purge device class
					for _, item := range hostbucket.Items {
						for _, device := range osdcrushdump.Devices {
							if device.ID == item.ID {
								for _, osdmetadata := range osdsmetadata {
									if osdmetadata.ID == device.ID {
										device.Info = osdmetadata
										osddevices = append(osddevices, device)
									}

								}
							}
						}
					}
				}
			}
			if len(osddevices) == 0 {
				log.Fatalf("Defined host not exist in root for rule: %v pool: %v", crushrulename, poolinfo.Pool)
			}
		}
	} else {
		for _, hostbucket := range bucketitems {
			for _, item := range hostbucket.Items {
				for _, device := range osdcrushdump.Devices {
					if device.ID == item.ID {
						for _, osdmetadata := range osdsmetadata {
							if osdmetadata.ID == device.ID {
								device.Info = osdmetadata
							}

						}
						osddevices = append(osddevices, device)
					}
				}
			}
		}
		if len(osddevices) == 0 {
			log.Fatalf("Osd not exist in root for rule: %v pool: %v", crushrulename, poolinfo.Pool)
		}
	}
	return osddevices
}

func ContainsPg(pgs []PlacementGroup, i int64) bool {
	for _, pg := range pgs {
		if i == pg.ActingPrimary {
			return true
		}
	}
	return false
}

func GetOsds(cephconn *Cephconnection, params Params) []Device {
	poolinfo := GetPoolSize(cephconn, params)
	if poolinfo.Size != 1 {
		log.Fatalf("Pool size must be 1. Current size for pool %v is %v. Don't forget that it must be useless pool (not production). Do:\n # ceph osd pool set %v min_size 1\n # ceph osd pool set %v size 1",
			poolinfo.Pool, poolinfo.Size, poolinfo.Pool, poolinfo.Pool)
	}
	placementGroups := GetPgByPool(cephconn, params)
	crushosddump := GetOsdCrushDump(cephconn)
	osddump := GetOsdDump(cephconn)
	osdsmetadata := GetOsdMetadata(cephconn)
	osddevices := GetOsdForLocations(params, crushosddump, osddump, poolinfo, osdsmetadata)
	for _, device := range osddevices {
		if exist := ContainsPg(placementGroups, device.ID); exist == false {
			log.Fatalln("Not enough pg for test. Some osd haven't placement group at all. Increase pg_num and pgp_num")
		}
	}
	return osddevices
}
