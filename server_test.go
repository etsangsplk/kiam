// Copyright 2017 uSwitch
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package kiam

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/fortytw2/leaktest"
	"github.com/uswitch/kiam/pkg/creds"
	kh "github.com/uswitch/kiam/pkg/http"
	"github.com/uswitch/kiam/pkg/testutil"
	"github.com/vmg/backoff"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
)

func TestParseAddress(t *testing.T) {
	ip, err := kh.ParseClientIP("127.0.0.1:9000")
	if err != nil {
		t.Fatal(err.Error())
	}

	if ip != "127.0.0.1" {
		t.Error("incorrect ip, was", ip)
	}
}

func TestPassthroughToMetadata(t *testing.T) {
	testutil.WithAWS(&testutil.AWSMetadata{InstanceID: "i-12345"}, context.Background(), func(ctx context.Context) {
		server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(nil), nil)
		go server.Serve()
		waitForServer(defaultConfig(), t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer server.Stop(ctx)

		body, status, err := get("/latest/meta-data/instance-id")
		if err != nil {
			t.Error(err)
		}
		if status != http.StatusOK {
			t.Error("should have returned ok, was", status)
		}
		if string(body) != "i-12345" {
			t.Error("should have returned metadata response, was", string(body))
		}
	})
}

func TestReturnsHealthStatus(t *testing.T) {
	testutil.WithAWS(&testutil.AWSMetadata{InstanceID: "i-12345"}, context.Background(), func(ctx context.Context) {
		server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(nil), nil)
		go server.Serve()
		waitForServer(defaultConfig(), t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		defer server.Stop(ctx)

		body, status, err := get("/health")
		if err != nil {
			t.Error("error retrieving health page:", err.Error())
		}
		if status != http.StatusOK {
			t.Error("expected 200 status code, was", status)
		}
		if string(body) != "i-12345" {
			t.Errorf("expected instance-id in response, was %s", string(body))
		}
	})
}

func TestReturnRoleForPod(t *testing.T) {
	defer leaktest.Check(t)()

	server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(testutil.NewPodWithRole("", "", "", "", "foo_role")), nil)
	go server.Serve()
	waitForServer(defaultConfig(), t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop(ctx)

	body, status, err := get("/latest/meta-data/iam/security-credentials/")
	if err != nil {
		t.Error("error retrieving role:", err.Error())
	}
	if status != http.StatusOK {
		t.Error("expected 200 response, was", status)
	}
	if string(body) != "foo_role" {
		t.Error("expected foo_role in body, was", string(body))
	}
}

func TestReturnNotFoundWhenNoPodFound(t *testing.T) {
	defer leaktest.Check(t)()

	server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(nil), nil)
	go server.Serve()
	waitForServer(defaultConfig(), t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop(ctx)

	_, status, err := get("/latest/meta-data/iam/security-credentials/")
	if err != nil {
		t.Error("error retrieving role:", err.Error())
	}
	if status != http.StatusNotFound {
		t.Error("expected 404 response, was", status)
	}
}

func TestReturnNotFoundWhenPodNotFoundAndRequestingCredentials(t *testing.T) {
	defer leaktest.Check(t)()

	server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(nil), nil)
	go server.Serve()
	waitForServer(defaultConfig(), t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop(ctx)

	_, status, err := get("/latest/meta-data/iam/security-credentials/dummyrole")
	if err != nil {
		t.Error("error retrieving role:", err.Error())
	}
	if status != http.StatusNotFound {
		t.Error("expected 404 response, was", status)
	}
}

func TestReturnsNotFoundResponseWithEmptyRole(t *testing.T) {
	defer leaktest.Check(t)()

	server := kh.NewWebServer(defaultConfig(), testutil.NewStubFinder(testutil.NewPod("", "", "", "")), nil)
	go server.Serve()
	waitForServer(defaultConfig(), t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop(ctx)

	_, status, err := get("/latest/meta-data/iam/security-credentials/")
	if err != nil {
		t.Error("error retrieving role:", err.Error())
	}
	if status != http.StatusNotFound {
		t.Error("expected 404 response, was", status)
	}
}

func TestReturnsCredentials(t *testing.T) {
	// defer leaktest.Check(t)()
	// fails because go-metrics leaks a ticker

	podFinder := testutil.NewStubFinder(testutil.NewPodWithRole("ns", "name", "192.168.0.1", "Running", "foo_role"))
	issuer := testutil.NewStubIssuer(func(role string) (*creds.Credentials, error) {
		return &creds.Credentials{
			AccessKeyId: "test",
		}, nil
	})
	server := kh.NewWebServer(defaultConfig(), podFinder, issuer)
	go server.Serve()
	waitForServer(defaultConfig(), t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer server.Stop(ctx)

	body, status, err := get("/latest/meta-data/iam/security-credentials/foo_role?ip=192.168.0.1")
	if err != nil {
		t.Error(err)
	}
	if status != http.StatusOK {
		t.Error("was unexpected response", status, string(body))
	}
	var c creds.Credentials
	json.Unmarshal(body, &c)

	if c.AccessKeyId != "test" {
		t.Error("expected access key to be set, was", c.AccessKeyId)
	}
}

func waitForServer(config *kh.ServerConfig, t *testing.T) {
	op := func() error {
		_, status, err := get("/ping")
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("unhealthy response")
		}
		return nil
	}

	err := backoff.Retry(op, backoff.NewConstantBackOff(time.Millisecond))
	if err != nil {
		t.Fatal("server unavailable in time")
	}
}

func defaultConfig() *kh.ServerConfig {
	return &kh.ServerConfig{
		ListenPort:       3129,
		MetadataEndpoint: "http://localhost:3199",
		AllowIPQuery:     true,
		MaxElapsedTime:   time.Second,
	}
}

func get(path string) ([]byte, int, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:3129%s", path))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	return body, resp.StatusCode, nil
}
