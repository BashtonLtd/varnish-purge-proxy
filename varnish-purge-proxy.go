package main

/*
 * varnish-purge-proxy
 * (C) Copyright Bashton Ltd, 2016
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
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"

	"gopkg.in/alecthomas/kingpin.v1"
)

var (
	port            = kingpin.Flag("port", "Port to listen on.").Default("8000").Int()
	cache           = kingpin.Flag("cache", "Time in seconds to cache instance IP lookup.").Default("60").Int()
	destport        = kingpin.Flag("destport", "The destination port of the varnish server to target.").Default("80").Int()
	listen          = kingpin.Flag("listen", "Host address to listen on, defaults to 127.0.0.1").Default("127.0.0.1").String()
	region          = kingpin.Flag("region", "Google region to discover varnish servers").Required().String()
	project         = kingpin.Flag("project", "Google project to discover varnish servers").Required().String()
	debug           = kingpin.Flag("debug", "Log additional debug messages.").Bool()
	credentials     = kingpin.Flag("credentials", "Path to service account JSON credentials").Required().String()
	nameprefix      = kingpin.Flag("nameprefix", "Instance name prefix, eg. varnish").Default("varnish").String()
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

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", *credentials)
	src, err := google.DefaultTokenSource(oauth2.NoContext, compute.ComputeReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to acquire token source: %v", err)
		return
	}

	oauthClient := oauth2.NewClient(context.Background(), src)

	service, err := compute.New(oauthClient)
	if err != nil {
		log.Fatalf("Unable to get client: ", err)
		return
	}

	go serveHTTP(*port, *listen, service)

	select {}
}

func serveHTTP(port int, host string, service *compute.Service) {
	timeout := time.Duration(5 * time.Second)
	client := http.Client{
		Timeout: timeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestHandler(w, r, &client, service)
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

func requestHandler(w http.ResponseWriter, r *http.Request, client *http.Client, service *compute.Service) {
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
		privateIPs = getPrivateIPs(service)
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

func getPrivateIPs(service *compute.Service) []string {
	instances := []string{}
	ctx := context.Background()

	zones := make([]string, 0)
	{
		call := service.Zones.List(*project)
		call.Filter("region eq .*" + *region + ".*")
		if err := call.Pages(ctx, func(page *compute.ZoneList) error {
			for _, v := range page.Items {
				log.Printf("Found zone: %s", v.Name)
				zones = append(zones, v.Name)
			}
			return nil
		}); err != nil {
			log.Fatalf("Failed to list zones: ", err)
		}
	}

	for _, zone := range zones {
		log.Printf("Checking zone: %s", zone)
		call := service.Instances.List(*project, zone)
		call.Filter("(name eq .*" + *nameprefix + ".*) (status eq RUNNING)")
		if err := call.Pages(ctx, func(page *compute.InstanceList) error {
			for _, v := range page.Items {
				log.Printf("Found instance: %s", v.Name)
				for _, n := range v.NetworkInterfaces {
					log.Printf("Found address: %s", n.NetworkIP)
					instances = append(instances, n.NetworkIP)
				}
			}
			return nil
		}); err != nil {
			log.Fatalf("Failed to list instances: ", err)
		}
	}

	return instances
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
	io.Copy(ioutil.Discard, response.Body)
	defer response.Body.Close()
	return
}
