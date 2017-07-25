// Copyright 2017 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

// package dashapi defines data structures used in dashboard communication
// and provides client interface.
package dashapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

type Dashboard struct {
	Client string
	Addr   string
	Key    string
}

func New(client, addr, key string) *Dashboard {
	return &Dashboard{
		Client: client,
		Addr:   addr,
		Key:    key,
	}
}

// Build describes all aspects of a kernel build.
type Build struct {
	Manager         string
	ID              string
	SyzkallerCommit string
	CompilerID      string
	KernelRepo      string
	KernelBranch    string
	KernelCommit    string
	KernelConfig    []byte
}

func (dash *Dashboard) UploadBuild(build *Build) error {
	return dash.query("upload_build", build, nil)
}

// Crash describes a single kernel crash (potentially with repro).
type Crash struct {
	BuildID     string // refers to Build.ID
	Title       string
	Maintainers []string
	Log         []byte
	Report      []byte
	// The following is optional and is filled only after repro.
	ReproOpts []byte
	ReproSyz  []byte
	ReproC    []byte
}

func (dash *Dashboard) ReportCrash(crash *Crash) error {
	return dash.query("report_crash", crash, nil)
}

// FailedRepro describes a failed repro attempt.
type FailedRepro struct {
	Manager string
	BuildID string
	Title   string
}

func (dash *Dashboard) ReportFailedRepro(repro *FailedRepro) error {
	return dash.query("report_failed_repro", repro, nil)
}

type LogEntry struct {
	Name string
	Text string
}

// Centralized logging on dashboard.
func (dash *Dashboard) LogError(name, msg string, args ...interface{}) {
	req := &LogEntry{
		Name: name,
		Text: fmt.Sprintf(msg, args...),
	}
	dash.query("log_error", req, nil)
}

// BugReport describes a single bug.
// Used by dashboard external reporting.
type BugReport struct {
	Config       []byte
	ID           string
	Title        string
	Maintainers  []string
	CompilerID   string
	KernelRepo   string
	KernelBranch string
	KernelCommit string
	Log          []byte
	Report       []byte
	KernelConfig []byte
	ReproC       []byte
	ReproSyz     []byte
}

type BugUpdate struct {
	ID         string
	Status     BugStatus
	ReproLevel ReproLevel
	DupOf      string
}

type PollRequest struct {
	Type string
}

type PollResponse struct {
	Reports []*BugReport
}

type (
	BugStatus  int
	ReproLevel int
)

const (
	BugStatusOpen BugStatus = iota
	BugStatusUpstream
	BugStatusInvalid
	BugStatusDup
)

const (
	ReproLevelNone ReproLevel = iota
	ReproLevelSyz
	ReproLevelC
)

func (dash *Dashboard) query(method string, req, reply interface{}) error {
	return Query(dash.Client, dash.Addr, dash.Key, method,
		http.NewRequest, http.DefaultClient.Do, req, reply)
}

type (
	RequestCtor func(method, url string, body io.Reader) (*http.Request, error)
	RequestDoer func(req *http.Request) (*http.Response, error)
)

func Query(client, addr, key, method string, ctor RequestCtor, doer RequestDoer, req, reply interface{}) error {
	values := make(url.Values)
	values.Add("client", client)
	values.Add("key", key)
	values.Add("method", method)
	var body io.Reader
	gzipped := false
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %v", err)
		}
		if len(data) < 100 || addr == "" || strings.HasPrefix(addr, "http://localhost:") {
			// Don't bother compressing tiny requests.
			// Don't compress for dev_appserver which does not support gzip.
			body = bytes.NewReader(data)
		} else {
			buf := new(bytes.Buffer)
			gz := gzip.NewWriter(buf)
			if _, err := gz.Write(data); err != nil {
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
			body = buf
			gzipped = true
		}
	}
	url := fmt.Sprintf("%v/api?%v", addr, values.Encode())
	r, err := ctor("POST", url, body)
	if err != nil {
		return err
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		if gzipped {
			r.Header.Set("Content-Encoding", "gzip")
		}
	}
	resp, err := doer(r)
	if err != nil {
		return fmt.Errorf("http request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("request failed with %v: %s", resp.Status, data)
	}
	if reply != nil {
		if err := json.NewDecoder(resp.Body).Decode(reply); err != nil {
			return fmt.Errorf("failed to unmarshal response: %v", err)
		}
	}
	return nil
}
