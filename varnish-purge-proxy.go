package main // import "github.com/madedotcom/varnish-purge-proxy"

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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	port            = kingpin.Flag("port", "Port to listen on.").Default("8000").Int()
	cache           = kingpin.Flag("cache", "Time in seconds to cache instance IP lookup.").Default("60").Int()
    listen          = kingpin.Flag("listen", "host address to listen on, defaults to 127.0.0.1").Default("127.0.0.1").String()
	tags            = kingpin.Arg("tag", "Key:value pair of tags to match EC2 instances.").Strings()
	region          aws.Region
	ResetAfter      time.Time
	TaggedInstances = []string{}
)

type Config struct {
	ConnectTimeout   time.Duration
	ReadWriteTimeout time.Duration
}

func init() {
	regionname := aws.InstanceRegion()
	region = aws.Regions[regionname]
}

func main() {
	kingpin.Version("1.2.1")
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

	// Set up access to ec2
	auth, err := aws.GetAuth("", "", "", time.Now().Add(time.Duration(24*365*time.Hour)))
	if err != nil {
		log.Println(err)
		return
	}
	ec2region := ec2.New(auth, region)

	go serveHTTP(*port, *listen, ec2region)

	select {}
}

func timeoutDialer(config *Config) func(net, addr string) (c net.Conn, err error) {
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
	config := &Config{
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
		go forwardRequest(r, ip, *client, requesturl, responseChannel)
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

func forwardRequest(r *http.Request, ip string, client http.Client, requesturl string, responseChannel chan int) {
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
	defer response.Body.Close()
	responseChannel <- response.StatusCode
	return
}
