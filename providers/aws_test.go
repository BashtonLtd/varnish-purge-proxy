package providers

import (
	"reflect"
	"testing"
)

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

func expect(t *testing.T, k string, a interface{}, b interface{}) {
	if a != b {
		t.Fatalf("%s: Expected %v (type %v) - Got %v (type %v)", k, b, reflect.TypeOf(b), a, reflect.TypeOf(a))
	}
}

func TestBuildFilter(t *testing.T) {
	testTags := []string{"machinetype:varnish", "env:stage"}
	awsService := AWSProvider{
		Tags: testTags,
	}

	filter, err := awsService.buildFilter()
	expect(t, "buildfilter", err, nil)
	expect(t, "buildfilter", *filter[0].Name, "tag:machinetype")
	value0 := filter[0].Values[0]
	expect(t, "buildfilter", *value0, "varnish")
	expect(t, "buildfilter", *filter[1].Name, "tag:env")
	value1 := filter[1].Values[0]
	expect(t, "buildfilter", *value1, "stage")
}

func TestBuildFilterInvalid(t *testing.T) {
	testTags := []string{"machinetypevarnish", "env:stage"}
	awsService := AWSProvider{
		Tags: testTags,
	}
	_, err := awsService.buildFilter()
	expect(t, "buildfilterinvalid", err.Error(), "expected TAG:VALUE got machinetypevarnish")
}
