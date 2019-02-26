package main

import (
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

//future feature
func makeoffsets(threads int64, bs int64, objsize int64) [][]int64 {
	var offsets [][]int64
	for i := int64(0); i < threads; i++ {
		s1 := rand.NewSource(i)
		r1 := rand.New(s1)
		localoffsets := make([]int64, 0, objsize-bs)
		for i := int64(0); i < objsize-bs; i += bs {
			localoffsets = append(localoffsets, i)
		}
		r1.Shuffle(len(localoffsets), func(i, j int) {
			localoffsets[i], localoffsets[j] = localoffsets[j], localoffsets[i]
		})
		offsets = append(offsets, localoffsets)
	}
	return offsets
}

func bench(cephconn *Cephconnection, osddevice Device, buffs *[][]byte, offset [][]int64, params *Params,
	wg *sync.WaitGroup, result chan []string) {
	defer wg.Done()
	threadresult := make(chan string, params.threadsCount)
	var osdresults, objectnames []string

	// calculate object for each thread
	for suffix := 0; len(objectnames) < int(params.threadsCount); suffix++ {
		name := "bench_" + strconv.Itoa(suffix)
		if osddevice.ID == GetObjActingPrimary(cephconn, *params, name) {
			objectnames = append(objectnames, name)
		}
	}
	for i, j := 0, 0; i < int(params.threadsCount); i, j = i+1, j+2 {
		go bench_thread(cephconn, osddevice, (*buffs)[j:j+2], offset[i], params, threadresult, objectnames[i])
	}
	for i := int64(0); i < params.threadsCount; i++ {
		osdresults = append(osdresults, <-threadresult)
	}
	close(threadresult)
	result <- osdresults
}

func bench_thread(cephconn *Cephconnection, osddevice Device, buffs [][]byte, offsets []int64, params *Params,
	result chan string, objname string) {
	defer cephconn.ioctx.Delete(objname)
	starttime := time.Now()
	var latencies []time.Duration
	endtime := starttime.Add(params.duration)
	n := 0
	for {
		if time.Now().After(endtime) {
			break
		}
		for _, offset := range offsets {
			if time.Now().Before(endtime) {
				startwritetime := time.Now()
				err := cephconn.ioctx.Write(objname, buffs[n], uint64(offset))
				endwritetime := time.Now()
				if err != nil {
					log.Printf("Can't write obj: %v, osd: %v", objname, osddevice.Name)
					continue
				}
				latencies = append(latencies, endwritetime.Sub(startwritetime))
			} else {
				break
			}
		}
		if n == 0 {
			n++
		} else {
			n = 0
		}
	}
	result <- fmt.Sprintf("Host: %v Osdname: %v Object: %v\n Latencies: %v\n Writes: %v", osddevice.Info.Hostname,
		osddevice.Name, objname, latencies, len(latencies))
}

func main() {
	params := Route()
	cephconn := connectioninit(params)
	defer cephconn.conn.Shutdown()

	// https://tracker.ceph.com/issues/24114
	time.Sleep(time.Millisecond * 100)

	var buffs [][]byte
	for i := int64(0); i < 2*params.threadsCount; i++ {
		buffs = append(buffs, make([]byte, params.blocksize))
	}
	for num := range buffs {
		_, err := rand.Read(buffs[num])
		if err != nil {
			log.Fatalln(err)
		}
	}
	osddevices := GetOsds(cephconn, params)
	offsets := makeoffsets(params.threadsCount, params.blocksize, params.objectsize)

	var wg sync.WaitGroup
	results := make(chan []string, len(osddevices)*int(params.threadsCount))
	for _, osd := range osddevices {
		wg.Add(1)
		if params.parallel == true {
			go bench(cephconn, osd, &buffs, offsets, &params, &wg, results)
		} else {
			bench(cephconn, osd, &buffs, offsets, &params, &wg, results)
			log.Printf("%v \n", <-results)
		}

	}

	if params.parallel == true {
		go func() {
			wg.Wait()
			time.Sleep(time.Second)
			close(results)
		}()

		for message := range results {
			log.Printf("%v \n", message)
		}
	}

}
