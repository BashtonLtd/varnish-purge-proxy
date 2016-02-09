package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func expect(t *testing.T, a interface{}, b interface{}) {
	if a != b {
		t.Fatalf("Expected %v (type %v) - Got %v (type %v)", b, reflect.TypeOf(b), a, reflect.TypeOf(a))
	}
}

func TestForwardRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w, "")
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf(err.Error())
	}
	host, strport, _ := net.SplitHostPort(u.Host)
	port, err := strconv.Atoi(strport)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// build request
	request, err := http.NewRequest("GET", "http://127.0.0.1", nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	client := newTimeoutClient()
	channel := make(chan int, 10)

	forwardRequest(request, host, port, *client, "/", channel)
	statusCode := <-channel
	expect(t, statusCode, 200)
}

func TestForwardRequestBrokenURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprintln(w, "")
	}))
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf(err.Error())
	}
	host, strport, _ := net.SplitHostPort(u.Host)
	port, err := strconv.Atoi(strport)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// build request
	request, err := http.NewRequest("GET", "http://127.0.0.1", nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	client := newTimeoutClient()
	channel := make(chan int, 10)

	forwardRequest(request, host, port, *client, "/%", channel)
	statusCode := <-channel
	expect(t, statusCode, 500)
}

func TestForwardRequestNoResponse(t *testing.T) {
	// build request
	request, err := http.NewRequest("GET", "http://127.0.0.1", nil)
	if err != nil {
		t.Fatalf(err.Error())
	}
	client := newTimeoutClient()
	channel := make(chan int, 10)

	forwardRequest(request, "127.0.0.1", 1234, *client, "/", channel)
	statusCode := <-channel
	expect(t, statusCode, 500)
}

func TestBuildFilter(t *testing.T) {
	testTags := []string{"machinetype:varnish", "env:stage"}
	filter, err := buildFilter(testTags)
	expect(t, err, nil)
	expect(t, *filter[0].Name, "machinetype")
	value0 := filter[0].Values[0]
	expect(t, *value0, "varnish")
	expect(t, *filter[1].Name, "env")
	value1 := filter[1].Values[0]
	expect(t, *value1, "stage")
}

func TestBuildFilterInvalid(t *testing.T) {
	testTags := []string{"machinetypevarnish", "env:stage"}
	_, err := buildFilter(testTags)
	expect(t, err.Error(), "expected TAG:VALUE got machinetypevarnish")
}
