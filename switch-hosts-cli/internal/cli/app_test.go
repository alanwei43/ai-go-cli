package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// Stop the existing infinite loop at its sleep boundary without waiting in tests.
func executeCycles(t *testing.T, app *App, args []string, cycles int, interval time.Duration) {
	t.Helper()
	stop := &struct{ stopped bool }{}
	sleeps := 0
	app.sleepFn = func(got time.Duration) {
		if got != interval {
			t.Fatalf("interval = %v, want %v", got, interval)
		}
		sleeps++
		if sleeps == cycles {
			panic(stop)
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil && recovered != stop {
			panic(recovered)
		}
	}()
	cmd := app.NewRootCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	t.Fatal("command returned before completing cycles")
}

func TestSubscriptionCycles(t *testing.T) {
	const url = "https://example.test/hosts?team=office"
	tests := []struct {
		name          string
		subscribeOnly bool
		service       bool
		noIPs         bool
		localError    bool
		pushFailure   string
		getFailure    string
		wantMethods   []string
		wantErrors    []string
	}{
		{name: "sync without subscription", wantMethods: []string{"PUT"}},
		{name: "service with unchanged IP still subscribes", service: true, wantMethods: []string{"PUT", "GET", "GET", "GET"}},
		{name: "no matching IP still subscribes", service: true, noIPs: true, wantMethods: []string{"GET", "GET", "GET"}},
		{name: "local IP error still subscribes", service: true, localError: true, wantMethods: []string{"GET", "GET", "GET"}, wantErrors: []string{"list addresses failed"}},
		{name: "failed push retries and subscribes", service: true, pushFailure: "status", wantMethods: []string{"PUT", "GET", "PUT", "GET", "GET"}, wantErrors: []string{"sync host info failed: status=503"}},
		{name: "push network error retries", service: true, pushFailure: "network", wantMethods: []string{"PUT", "GET", "PUT", "GET", "GET"}, wantErrors: []string{"sync host info:", "network unavailable"}},
		{name: "failed subscription retries with unchanged IP", service: true, getFailure: "status", wantMethods: []string{"PUT", "GET", "GET", "GET"}, wantErrors: []string{"request remote hosts failed: status=503"}},
		{name: "subscription network error retries", service: true, getFailure: "network", wantMethods: []string{"PUT", "GET", "GET", "GET"}, wantErrors: []string{"request remote hosts:", "network unavailable"}},
		{name: "both failures are logged", service: true, pushFailure: "status", getFailure: "status", wantMethods: []string{"PUT", "GET", "PUT", "GET", "GET"}, wantErrors: []string{"sync host info failed: status=503", "request remote hosts failed: status=503"}},
		{name: "standalone subscribe retries status failure", subscribeOnly: true, getFailure: "status", wantMethods: []string{"GET", "GET", "GET"}, wantErrors: []string{"request remote hosts failed: status=503"}},
		{name: "standalone subscribe retries network failure", subscribeOnly: true, getFailure: "network", wantMethods: []string{"GET", "GET", "GET"}, wantErrors: []string{"request remote hosts:", "network unavailable"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := NewApp()
			app.hostnameFn = func() (string, error) { return "devbox", nil }
			var stdout, stderr bytes.Buffer
			app.stdout, app.stderr = &stdout, &stderr
			app.hostsPath = filepath.Join(t.TempDir(), "hosts")
			const original = "127.0.0.1 localhost\n"
			if err := os.WriteFile(app.hostsPath, []byte(original), 0o644); err != nil {
				t.Fatal(err)
			}
			app.localIPsFn = func(prefix string) ([]string, error) {
				if prefix != "192.168.0.0/16" {
					t.Fatalf("IP prefix = %q", prefix)
				}
				if tt.localError {
					return nil, errors.New("list addresses failed")
				}
				if tt.noIPs {
					return nil, nil
				}
				return []string{"192.168.1.20", "192.168.1.30"}, nil
			}
			var methods []string
			pushes, gets := 0, 0
			app.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != url {
					t.Fatalf("request URL = %q, want %q", req.URL, url)
				}
				methods = append(methods, req.Method)
				failure := ""
				body := ""
				if req.Method == http.MethodGet {
					gets++
					if gets == 1 {
						failure = tt.getFailure
					}
					body = fmt.Sprintf("192.168.1.%d service.local\n", gets)
				} else {
					pushes++
					if pushes == 1 {
						failure = tt.pushFailure
					}
					var payload syncPayload
					if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if req.Header.Get("Content-Type") != "application/json" || payload.HostName != "devbox" || !reflect.DeepEqual(payload.IP, []string{"192.168.1.20"}) {
						t.Fatalf("unexpected sync payload or content type: %+v, %v", payload, req.Header)
					}
				}
				if failure == "network" {
					return nil, errors.New("network unavailable")
				}
				status := http.StatusOK
				if failure == "status" {
					status, body = http.StatusServiceUnavailable, "temporarily unavailable"
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			args := []string{"sync", url, "--host-name", "devbox", "--interval", "17"}
			blockName := hashText(url)
			if tt.service {
				args = []string{"service", url, "--interval", "17"}
			}
			if tt.subscribeOnly {
				blockName = "office"
				args = []string{"subscribe", url, "--name", blockName, "--interval", "17"}
			}
			executeCycles(t, app, args, 3, 17*time.Second)
			if !reflect.DeepEqual(methods, tt.wantMethods) {
				t.Fatalf("request methods = %v, want %v", methods, tt.wantMethods)
			}
			for _, want := range tt.wantErrors {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want %q", stderr.String(), want)
				}
			}
			if len(tt.wantErrors) == 0 && stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %s", &stderr)
			}
			content, err := os.ReadFile(app.hostsPath)
			if err != nil {
				t.Fatal(err)
			}
			want := original
			if tt.service || tt.subscribeOnly {
				want += fmt.Sprintf("\n# switch-hosts-cli start %s\n192.168.1.3 service.local\n# switch-hosts-cli end %s\n", blockName, blockName)
				backup, err := os.ReadFile(filepath.Join(filepath.Dir(app.hostsPath), "hosts_backup", "hosts."+hashText(original)))
				if err != nil || string(backup) != original {
					t.Fatalf("original backup = %q, error = %v", backup, err)
				}
			}
			if string(content) != want {
				t.Fatalf("hosts content = %q, want %q", content, want)
			}
		})
	}
}

func TestServiceAddSystemdDelegatesWithoutForegroundRequests(t *testing.T) {
	app := NewApp()
	called := false
	app.addSystemdFn = func(url string, interval int) error {
		called = true
		if url != "https://example.test/hosts" || interval != 17 {
			t.Fatalf("add-systemd arguments = %q, %d", url, interval)
		}
		return nil
	}
	app.hostnameFn = func() (string, error) { t.Fatal("must not run foreground sync"); return "", nil }
	cmd := app.NewRootCmd()
	cmd.SetArgs([]string{"service", "https://example.test/hosts", "--interval", "17", "--add-systemd"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("systemd manager was not called")
	}
}

func TestServiceDelSystemdDelegatesWithoutForegroundRequests(t *testing.T) {
	for _, args := range [][]string{
		{"service", "--del-systemd"},
		{"service", "https://example.test/hosts", "--del-systemd"},
		{"service", "--interval", "0", "--del-systemd"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app := NewApp()
			called := false
			wantErr := errors.New("delete failed")
			app.delSystemdFn = func() error { called = true; return wantErr }
			app.addSystemdFn = func(string, int) error { t.Fatal("must not install"); return nil }
			app.hostnameFn = func() (string, error) { t.Fatal("must not sync"); return "", nil }
			cmd := app.NewRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			if err := cmd.Execute(); !errors.Is(err, wantErr) || !called {
				t.Fatalf("delete called = %v, error = %v", called, err)
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"sync", "https://example.test/hosts", "--with-subscribe"}, "unknown flag: --with-subscribe"},
		{[]string{"service"}, "accepts 1 arg"},
		{[]string{"service", "--add-systemd"}, "accepts 1 arg"},
		{[]string{"service", "https://example.test/hosts", "--systemctl"}, "unknown flag: --systemctl"},
		{[]string{"service", "--add-systemd", "--del-systemd"}, "were all set"},
		{[]string{"service", "https://example.test/hosts", "--add-systemd", "--del-systemd"}, "were all set"},
		{[]string{"service", "--add-systemd=false", "--del-systemd"}, "were all set"},
		{[]string{"service", "url", "extra", "--del-systemd"}, "accepts at most 1 arg"},
		{[]string{"service", "https://example.test/hosts", "extra"}, "accepts 1 arg"},
		{[]string{"service", "https://example.test/hosts", "--interval", "0"}, "greater than 0"},
		{[]string{"service", "https://example.test/hosts", "--interval", "-1", "--add-systemd"}, "greater than 0"},
		{[]string{"service", "https://example.test/hosts"}, "resolve hostname"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			app := NewApp()
			app.hostnameFn = func() (string, error) { return "", errors.New("unavailable") }
			app.addSystemdFn = func(string, int) error { t.Fatal("must validate before installation"); return nil }
			app.delSystemdFn = func() error { t.Fatal("must validate before deletion"); return nil }
			cmd := app.NewRootCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
