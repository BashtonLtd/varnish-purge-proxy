package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"
)

func expect(t *testing.T, k string, a interface{}, b interface{}) {
	if a != b {
		t.Fatalf("%s: Expected %v (type %v) - Got %v (type %v)", k, b, reflect.TypeOf(b), a, reflect.TypeOf(a))
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

	cases := map[string]struct {
		url      string
		host     string
		port     int
		expected bool
	}{
		"success":    {"/", host, port, false},
		"brokenurl":  {"/%", host, port, true},
		"noresponse": {"/", host, 1234, true},
	}

	for k, tc := range cases {
		// build request
		request, err := http.NewRequest("GET", "http://127.0.0.1", nil)
		if err != nil {
			t.Fatalf(err.Error())
		}
		timeout := time.Duration(5 * time.Second)
		client := http.Client{
			Timeout: timeout,
		}
		channel := make(chan int, 10)

		var wg sync.WaitGroup
		wg.Add(1)
		forwardRequest(request, host, tc.port, client, tc.url, channel, &wg)
		errored := false
		select {
		case _, ok := <-channel:
			if ok {
				errored = true
			} else {
				errored = true
			}
		default:
			errored = false
		}
		expect(t, k, errored, tc.expected)
	}

}

func TestCopyRequest(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("X-Foo", "")

	dup, _ := copyRequest(req)
	expect(t, "host is copied", dup.Host, req.Host)
	expect(t, "header is copied", dup.Header.Get("X-Foo"), req.Header.Get("X-Foo"))
}
