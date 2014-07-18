package main

/*
 * varnish-purge-proxy
 * (C) Copyright Bashton Ltd, 2014
 *
 * varnish-purge-proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * varnish-purge-proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with varnish-purge-proxy.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
import (
	"fmt"
	"github.com/crowdmob/goamz/aws"
	"github.com/crowdmob/goamz/ec2"
	"gopkg.in/alecthomas/kingpin.v1"
	"log"
	"log/syslog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	port            = kingpin.Flag("port", "Port to listen on.").Default("8000").Int()
	cache           = kingpin.Flag("cache", "Time in seconds to cache instance IP lookup.").Default("60").Int()
	tags            = kingpin.Arg("tag", "Key:value pair of tags to match EC2 instances.").Strings()
	region          aws.Region
	ResetAfter      time.Time
	TaggedInstances = []string{}
)

func init() {
	regionname := aws.InstanceRegion()
	region = aws.Regions[regionname]
}

func main() {
	kingpin.Version("1.1.5")
	kingpin.Parse()

	sl, err := syslog.New(syslog.LOG_NOTICE, "[varnish-purge-proxy]")
	defer sl.Close()
	if err != nil {
		log.Println("Error writing to syslog")
	} else {
		log.SetOutput(sl)
	}

	if len(*tags) == 0 {
		fmt.Println("No tags specified")
		return
	}

	// Set up access to ec2
	auth, err := aws.GetAuth("", "", "", time.Now().Add(time.Duration(24*365*time.Hour)))
	if err != nil {
		log.Println(err)
		return
	}
	ec2region := ec2.New(auth, region)

	go serveHTTP(*port, ec2region)

	select {}
}

func serveHTTP(port int, ec2region *ec2.EC2) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestHandler(w, r, ec2region)
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Println("Listening for requests at", addr)
	err := http.ListenAndServe(addr, nil)
	log.Println(err.Error())
}

func requestHandler(w http.ResponseWriter, r *http.Request, ec2region *ec2.EC2) {
	// check that request is PURGE and has X-Purge-Regex header set
	if _, exists := r.Header["X-Purge-Regex"]; !exists || r.Method != "PURGE" {
		http.Error(w, http.StatusText(400), 400)
		return
	}

	privateIPs := TaggedInstances
	// Check instance cache
	if time.Now().After(ResetAfter) {
		privateIPs = getPrivateIPs(ec2region)
		ResetAfter = time.Now().Add(time.Duration(*cache*1000) * time.Millisecond)
		TaggedInstances = privateIPs
	}

	log.Printf("Sending PURGE to: %+v", privateIPs)
	// start gorountine for each server
	responseChannel := make(chan int)
	requesturl := fmt.Sprintf("%v", r.URL)
	for _, ip := range privateIPs {
		go forwardRequest(r, ip, requesturl, responseChannel)
	}

	// gather responses from all requests
	timeout := time.After(30 * time.Second)
	return500 := false
	responsesReceived := 0
ENDREQUEST:
	for {
		select {
		case response := <-responseChannel:
			responsesReceived++
			if response == 500 {
				return500 = true
				break ENDREQUEST
			}
			if responsesReceived == len(privateIPs) {
				break ENDREQUEST
			}

		case <-timeout:
			return500 = true
			break ENDREQUEST
		}
	}

	// send response to client
	if return500 == true {
		http.Error(w, http.StatusText(500), 500)
	}
	return
}

func getPrivateIPs(ec2region *ec2.EC2) []string {
	filter := ec2.NewFilter()

	for _, tag := range *tags {
		parts := strings.SplitN(tag, ":", 2)
		if len(parts) != 2 {
			log.Println("expected TAG:VALUE got", tag)
			break
		}
		filter.Add(fmt.Sprintf("tag:%v", parts[0]), parts[1])
	}

	taggedInstances := []string{}

	resp, err := ec2region.DescribeInstances(nil, filter)
	if err != nil {
		log.Println(err)
		return taggedInstances
	}

	for _, rsv := range resp.Reservations {
		for _, inst := range rsv.Instances {
			taggedInstances = append(taggedInstances, inst.PrivateIPAddress)
		}
	}
	return taggedInstances
}

func forwardRequest(r *http.Request, ip string, requesturl string, responseChannel chan int) {
	client := &http.Client{}
	r.Host = ip
	r.RequestURI = ""

	newURL, err := url.Parse(fmt.Sprintf("http://%v%v", ip, requesturl))
	if err != nil {
		log.Println(err)
		responseChannel <- 500
		return
	}
	r.URL = newURL
	response, err := client.Do(r)
	if err != nil {
		log.Println(err)
		responseChannel <- 500
		return
	}
	responseChannel <- response.StatusCode
}
