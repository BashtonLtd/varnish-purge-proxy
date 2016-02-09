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
	"log"
	"log/syslog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"gopkg.in/alecthomas/kingpin.v1"
)

var (
	port            = kingpin.Flag("port", "Port to listen on.").Default("8000").Int()
	cache           = kingpin.Flag("cache", "Time in seconds to cache instance IP lookup.").Default("60").Int()
	destport        = kingpin.Flag("destport", "The destination port of the varnish server to target.").Default("80").Int()
	listen          = kingpin.Flag("listen", "host address to listen on, defaults to 127.0.0.1").Default("127.0.0.1").String()
	tags            = kingpin.Arg("tag", "Key:value pair of tags to match EC2 instances.").Strings()
	region          string
	resetAfter      time.Time
	taggedInstances = []string{}
)

type config struct {
	ConnectTimeout   time.Duration
	ReadWriteTimeout time.Duration
}

func main() {
	kingpin.Version("1.2.2")
	kingpin.Parse()

	sl, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, "[varnish-purge-proxy]")
	defer sl.Close()
	if err != nil {
		log.Println("Error writing to syslog")
	} else {
		log.SetFlags(0)
		log.SetOutput(sl)
	}

	if len(*tags) == 0 {
		fmt.Println("No tags specified")
		return
	}

	region, err = ec2metadata.New(session.New()).Region()
	if err != nil {
		log.Printf("Unable to retrieve the region from the EC2 instance %v\n", err)
	}

	// Set up access to ec2
	svc := ec2.New(session.New(), &aws.Config{Region: &region})

	go serveHTTP(*port, *listen, svc)

	select {}
}

func timeoutDialer(config *config) func(net, addr string) (c net.Conn, err error) {
	return func(netw, addr string) (net.Conn, error) {
		conn, err := net.DialTimeout(netw, addr, config.ConnectTimeout)
		if err != nil {
			return nil, err
		}
		conn.SetDeadline(time.Now().Add(config.ReadWriteTimeout))
		return conn, nil
	}
}

func newTimeoutClient(args ...interface{}) *http.Client {
	// Default configuration
	config := &config{
		ConnectTimeout:   5 * time.Second,
		ReadWriteTimeout: 5 * time.Second,
	}

	// merge the default with user input if there is one
	if len(args) == 1 {
		timeout := args[0].(time.Duration)
		config.ConnectTimeout = timeout
		config.ReadWriteTimeout = timeout
	}

	if len(args) == 2 {
		config.ConnectTimeout = args[0].(time.Duration)
		config.ReadWriteTimeout = args[1].(time.Duration)
	}

	return &http.Client{
		Transport: &http.Transport{
			Dial: timeoutDialer(config),
		},
	}
}

func serveHTTP(port int, host string, ec2region *ec2.EC2) {
	client := newTimeoutClient()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestHandler(w, r, client, ec2region)
	})

	addr := fmt.Sprintf("%v:%d", host, port)
	server := &http.Server{
		Addr:           addr,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	log.Println("Listening for requests at", addr)
	err := server.ListenAndServe()
	log.Println(err.Error())
}

func requestHandler(w http.ResponseWriter, r *http.Request, client *http.Client, ec2region *ec2.EC2) {
	// check that request is PURGE and has X-Purge-Regex header set
	if _, exists := r.Header["X-Purge-Regex"]; !exists || r.Method != "PURGE" {
		http.Error(w, http.StatusText(400), 400)
		return
	}

	privateIPs := taggedInstances
	// Check instance cache
	if time.Now().After(resetAfter) {
		privateIPs = getPrivateIPs(ec2region)
		resetAfter = time.Now().Add(time.Duration(*cache*1000) * time.Millisecond)
		taggedInstances = privateIPs
	}

	log.Printf("Sending PURGE to: %+v", privateIPs)
	// start gorountine for each server
	responseChannel := make(chan int)
	requesturl := fmt.Sprintf("%v", r.URL)
	for _, ip := range privateIPs {
		go forwardRequest(r, ip, *destport, *client, requesturl, responseChannel)
	}

	return500 := getResponses(len(privateIPs), responseChannel)

	// send response to client
	if return500 == true {
		http.Error(w, http.StatusText(500), 500)
	}
	return
}

func getResponses(IPCount int, responseChannel chan int) bool {
	// gather responses from all requests
	timeout := time.After(30 * time.Second)
	return500 := false
	responsesReceived := 0

	for {
		select {
		case response := <-responseChannel:
			responsesReceived++
			if response == 500 {
				return500 = true
			}
			if responsesReceived == IPCount {
				return return500
			}

		case <-timeout:
			return500 = true
			return return500
		}
	}
}

func getPrivateIPs(ec2region *ec2.EC2) []string {
	instances := []string{}
	filters, err := buildFilter(*tags)
	if err != nil {
		log.Println(err)
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := ec2region.DescribeInstances(&request)

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, *instance.PrivateIpAddress)
		}
	}

	return instances
}

func buildFilter(tags []string) ([]*ec2.Filter, error) {
	filters := []*ec2.Filter{}

	for _, tag := range tags {
		parts := strings.SplitN(tag, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected TAG:VALUE got %s", tag)
		}
		filters = append(filters, &ec2.Filter{
			Name:   aws.String(parts[0]),
			Values: []*string{aws.String(parts[1])},
		})
	}
	return filters, nil

}

func forwardRequest(r *http.Request, ip string, port int, client http.Client, requesturl string, responseChannel chan int) {
	r.Host = ip
	r.RequestURI = ""

	newURL, err := url.Parse(fmt.Sprintf("http://%v:%d%v", ip, port, requesturl))
	if err != nil {
		responseChannel <- 500
		return
	}
	r.URL = newURL
	response, err := client.Do(r)
	if err != nil {
		responseChannel <- 500
		return
	}
	defer response.Body.Close()
	responseChannel <- response.StatusCode
	return
}
