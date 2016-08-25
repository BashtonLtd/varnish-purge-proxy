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
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	listen          = kingpin.Flag("listen", "Host address to listen on, defaults to 127.0.0.1").Default("127.0.0.1").String()
	tags            = kingpin.Arg("tag", "Key:value pair of tags to match EC2 instances.").Strings()
	debug           = kingpin.Flag("debug", "Log additional debug messages.").Bool()
	region          string
	resetAfter      time.Time
	taggedInstances = []string{}
)

type config struct {
	ConnectTimeout   time.Duration
	ReadWriteTimeout time.Duration
}

func main() {
	kingpin.Version("2.1.0")
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

func serveHTTP(port int, host string, ec2region *ec2.EC2) {
	timeout := time.Duration(5 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestHandler(w, r, &client, ec2region)
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
		if *debug {
			log.Printf("Error invalid request: %s, %s\n", r.Header, r.Method)
		}
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
	responseChannel := make(chan int, len(privateIPs))
	requesturl := fmt.Sprintf("%v", r.URL)

	var wg sync.WaitGroup
	wg.Add(len(privateIPs))

	for _, ip := range privateIPs {
		req, err := copyRequest(r)
		if err != nil {
			wg.Add(-1)
			if *debug {
				log.Println("Failed to copy request.")
			}
		} else {
			go forwardRequest(req, ip, *destport, *client, requesturl, responseChannel, &wg)
		}
	}

	wg.Wait()

	select {
	case _, ok := <-responseChannel:
		if ok {
			// Response channel contains at least one error
			http.Error(w, http.StatusText(500), 500)
		} else {
			// Channel has been closed
			http.Error(w, http.StatusText(500), 500)
		}
	default:
		return
	}
}

func copyRequest(src *http.Request) (*http.Request, error) {
	req, err := http.NewRequest(src.Method, src.URL.String(), src.Body)
	if err != nil {
		return nil, err
	}

	for k, vs := range src.Header {
		req.Header[k] = make([]string, len(vs))
		copy(req.Header[k], vs)
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
	}
	return req, nil
}

func getPrivateIPs(ec2region *ec2.EC2) []string {
	instances := []string{}
	filters, err := buildFilter(*tags)
	if err != nil {
		log.Println(err)
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := ec2region.DescribeInstances(&request)
	if err != nil {
		log.Println(err)
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.PrivateIpAddress != nil {
				if *debug {
					log.Printf("Adding %s to IP list\n", *instance.PrivateIpAddress)
				}
				instances = append(instances, *instance.PrivateIpAddress)
			}
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
		tagName := fmt.Sprintf("tag:%s", *aws.String(parts[0]))
		filters = append(filters, &ec2.Filter{
			Name:   &tagName,
			Values: []*string{aws.String(parts[1])},
		})
	}
	return filters, nil

}

func forwardRequest(r *http.Request, ip string, destport int, client http.Client, requesturl string, responseChannel chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	r.Host = ip
	r.RequestURI = ""

	newURL, err := url.Parse(fmt.Sprintf("http://%v:%d%v", ip, destport, requesturl))
	if err != nil {
		log.Printf("Error parsing URL: %s\n", err)
		if *debug {
			log.Printf("For URL: %s\n", fmt.Sprintf("http://%v:%d%v", ip, destport, requesturl))
		}
		responseChannel <- 500
		return
	}
	r.URL = newURL
	response, err := client.Do(r)
	if err != nil {
		log.Printf("Error sending request: %s\n", err)
		if *debug {
			log.Printf("For URL: %s\n", r.URL)
		}
		responseChannel <- 500
		return
	}
	defer response.Body.Close()
	return
}
