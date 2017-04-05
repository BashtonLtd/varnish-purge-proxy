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
	"io"
	"io/ioutil"
	"log"
	"log/syslog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/BashtonLtd/varnish-purge-proxy/providers"
	"gopkg.in/alecthomas/kingpin.v1"
)

var (
	// Global application args
	app      = kingpin.New("varnish-purge-proxy", "Proxy purge requests to multiple varnish servers.")
	cache    = app.Flag("cache", "Time in seconds to cache instance IP lookup.").Default("60").Int()
	debug    = app.Flag("debug", "Log additional debug messages.").Bool()
	destport = app.Flag("destport", "The destination port of the varnish server to target.").Default("80").Int()
	listen   = app.Flag("listen", "Host address to listen on, defaults to 127.0.0.1").Default("127.0.0.1").String()
	port     = app.Flag("port", "Port to listen on.").Default("8000").Int()

	// AWS service args
	awsService = app.Command("aws", "Use AWS service.")
	tags       = awsService.Arg("tag", "Key:value pair of tags to match EC2 instances.").Required().Strings()

	// GCE service args
	gceService  = app.Command("gce", "Use GCE service.")
	credentials = gceService.Flag("credentials", "Path to service account JSON credentials").Required().String()
	nameprefix  = gceService.Flag("nameprefix", "Instance name prefix, eg. varnish").Default("varnish").String()
	project     = gceService.Flag("project", "Google project to discover varnish servers").Required().String()
	region      = gceService.Flag("region", "Google region to discover varnish servers").Required().String()

	// Application variables
	resetAfter      time.Time
	service         providers.Service
	taggedInstances = []string{}
)

func main() {
	kingpin.Version("3.0.1")

	// Log to syslog
	sl, err := syslog.New(syslog.LOG_NOTICE|syslog.LOG_LOCAL0, "[varnish-purge-proxy]")
	defer sl.Close()
	if err != nil {
		log.Println("Error writing to syslog")
	} else {
		log.SetFlags(0)
		log.SetOutput(sl)
	}

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	// Register user
	case awsService.FullCommand():
		service = &providers.AWSProvider{
			Tags:  *tags,
			Debug: *debug,
		}
		err := service.Auth()
		if err != nil {
			log.Fatalln("Failed to Authenticate AWS Service:", err)
		}
	case gceService.FullCommand():
		service = &providers.GCEProvider{
			Credentials: *credentials,
			Debug:       *debug,
			NamePrefix:  *nameprefix,
			Project:     *project,
			Region:      *region,
		}
		err := service.Auth()
		if err != nil {
			log.Fatalln("Failed to Authenticate GCE Service:", err)
		}
	}

	go serveHTTP(*port, *listen, service)

	select {}
}

func serveHTTP(port int, host string, service providers.Service) {
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

func requestHandler(w http.ResponseWriter, r *http.Request, client *http.Client, service providers.Service) {
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
		privateIPs = service.GetPrivateIPs()
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
	req.Header.Set("Host", src.Host)
	return req, nil
}

func forwardRequest(r *http.Request, ip string, destport int, client http.Client, requesturl string, responseChannel chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	r.Host = r.Header.Get("Host")
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
